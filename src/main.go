package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"replychat/src/agents"
	"replychat/src/projectfs"
	"strings"
	"sync"
	"syscall"
	"time"

	_ "github.com/glebarez/go-sqlite"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/joho/godotenv"
)

//go:embed template/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

var parsedTemplates = template.Must(template.ParseFS(templateFS, "template/*.html"))

var db *sql.DB

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Client struct {
	conn      *websocket.Conn
	projectID string
	userID    string
	send      chan []byte
	hub       *Hub
}

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan []byte
	register   chan *Client
	unregister chan *Client
	mu         sync.RWMutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan []byte),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			log.Printf("ws: client registered, total: %d", len(h.clients))

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
			log.Printf("ws: client unregistered, total: %d", len(h.clients))

		case message := <-h.broadcast:
			h.mu.RLock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()

	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("ws: read error: %v", err)
			}
			break
		}

		handleMessage(c, message)
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			w.Write(message)

			n := len(c.send)
			for i := 0; i < n; i++ {
				w.Write([]byte{'\n'})
				w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}

		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func handleMessage(c *Client, message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("ws: invalid message format: %v", err)
		return
	}

	msgType, ok := msg["type"].(string)
	if !ok {
		log.Printf("ws: message missing type field")
		return
	}

	switch msgType {
	case "chat.message":
		handleChatMessage(c, msg)
	case "agent.command":
		handleAgentCommand(c, msg)
	default:
		log.Printf("ws: unknown message type: %s", msgType)
	}
}

func handleChatMessage(c *Client, msg map[string]interface{}) {
	payload, ok := msg["payload"].(map[string]interface{})
	if !ok {
		return
	}

	content, _ := payload["content"].(string)
	projectID, _ := payload["projectId"].(string)

	messageID := uuid.New().String()
	timestamp := time.Now()

	_, err := db.Exec(`
		INSERT INTO messages (id, project_id, sender_id, sender_type, content, message_type, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, messageID, projectID, c.userID, "user", content, "chat", timestamp)

	if err != nil {
		log.Printf("db: failed to save message: %v", err)
		return
	}

	response := map[string]interface{}{
		"type": "message.received",
		"payload": map[string]interface{}{
			"message": map[string]interface{}{
				"id":          messageID,
				"projectId":   projectID,
				"senderId":    c.userID,
				"senderType":  "user",
				"content":     content,
				"messageType": "chat",
				"timestamp":   timestamp,
			},
		},
	}

	responseJSON, _ := json.Marshal(response)
	c.hub.broadcast <- responseJSON

	go agents.ProcessMessage(db, c.hub.broadcast, projectID, content, c.userID)
}

func handleAgentCommand(c *Client, msg map[string]interface{}) {
	log.Printf("agent: command received: %v", msg)
}

func renderTemplate(w http.ResponseWriter, name string, data any) error {
	tmpl := parsedTemplates.Lookup(name)
	if tmpl == nil {
		return fmt.Errorf("template %q not found", name)
	}
	return tmpl.Execute(w, data)
}

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	renderTemplate(w, "index.html", nil)
}

func projectHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	projectID := r.URL.Query().Get("id")
	if projectID == "" {
		http.Redirect(w, r, "/projects", http.StatusTemporaryRedirect)
		return
	}

	var memberID string
	err = db.QueryRow(`
		SELECT id FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, userID).Scan(&memberID)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusTemporaryRedirect)
		return
	}

	var username, email string
	var projectName string
	db.QueryRow(`SELECT name, email FROM users WHERE id = ?`, userID).Scan(&username, &email)
	db.QueryRow(`SELECT name FROM projects WHERE id = ?`, projectID).Scan(&projectName)

	data := map[string]interface{}{
		"Username":    username,
		"Email":       email,
		"UserID":      userID,
		"ProjectID":   projectID,
		"ProjectName": projectName,
	}

	renderTemplate(w, "project.html", data)
}

func wsHandler(w http.ResponseWriter, r *http.Request, hub *Hub) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws: upgrade failed: %v", err)
		return
	}

	projectID := r.URL.Query().Get("projectId")
	if projectID == "" {
		projectID = "default"
	}

	client := &Client{
		conn:      conn,
		projectID: projectID,
		userID:    userID,
		send:      make(chan []byte, 256),
		hub:       hub,
	}

	hub.register <- client

	go client.writePump()
	go client.readPump()
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	email := r.FormValue("email")
	name := r.FormValue("name")

	if email == "" || name == "" {
		http.Error(w, "Email and name required", http.StatusBadRequest)
		return
	}

	var userID string
	err := db.QueryRow(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID)

	if err == sql.ErrNoRows {
		userID = uuid.New().String()
		_, err = db.Exec(`
			INSERT INTO users (id, email, name, created_at)
			VALUES (?, ?, ?, ?)
		`, userID, email, name, time.Now())

		if err != nil {
			log.Printf("db: failed to create user: %v", err)
			http.Error(w, "Database error", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		log.Printf("db: query error: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()
	_, err = db.Exec(`
		INSERT INTO sessions (id, user_id, created_at)
		VALUES (?, ?, ?)
	`, sessionID, userID, time.Now())

	if err != nil {
		log.Printf("db: failed to create session: %v", err)
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session_id",
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   86400 * 7,
	})

	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err == nil {
		db.Exec(`DELETE FROM sessions WHERE id = ?`, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session_id",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func kanbanHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	projectID := r.URL.Query().Get("id")
	if projectID == "" {
		http.Redirect(w, r, "/projects", http.StatusTemporaryRedirect)
		return
	}

	var memberID string
	err = db.QueryRow(`
		SELECT id FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, userID).Scan(&memberID)
	if err != nil {
		http.Redirect(w, r, "/projects", http.StatusTemporaryRedirect)
		return
	}

	var username, email string
	var projectName string
	db.QueryRow(`SELECT name, email FROM users WHERE id = ?`, userID).Scan(&username, &email)
	db.QueryRow(`SELECT name FROM projects WHERE id = ?`, projectID).Scan(&projectName)

	data := map[string]interface{}{
		"Username":    username,
		"Email":       email,
		"UserID":      userID,
		"ProjectID":   projectID,
		"ProjectName": projectName,
	}

	renderTemplate(w, "kanban.html", data)
}

func issuesAPIHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		listIssuesHandler(w, r)
	case http.MethodPost:
		createIssueHandler(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func listIssuesHandler(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		projectID = "default"
	}

	rows, err := db.Query(`
		SELECT id, title, description, priority, status,
		       created_by, created_by_type, assigned_agent_id,
		       queued_at, started_at, completed_at
		FROM issues
		WHERE project_id = ?
		ORDER BY
			CASE priority
				WHEN 'urgent' THEN 0
				WHEN 'high' THEN 1
				WHEN 'medium' THEN 2
				WHEN 'low' THEN 3
			END,
			queued_at DESC
	`, projectID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	issues := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, title, description, priority, status, createdBy, createdByType string
		var assignedAgentID sql.NullString
		var queuedAt, startedAt, completedAt sql.NullTime

		rows.Scan(&id, &title, &description, &priority, &status, &createdBy, &createdByType,
			&assignedAgentID, &queuedAt, &startedAt, &completedAt)

		issue := map[string]interface{}{
			"id":                id,
			"title":             title,
			"description":       description,
			"priority":          priority,
			"status":            status,
			"created_by":        createdBy,
			"created_by_type":   createdByType,
			"assigned_agent_id": assignedAgentID.String,
			"queued_at":         queuedAt.Time,
			"started_at":        startedAt.Time,
			"completed_at":      completedAt.Time,
		}
		issues = append(issues, issue)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"issues": issues,
	})
}

func createIssueHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectID       string `json:"project_id"`
		Title           string `json:"title"`
		Description     string `json:"description"`
		Priority        string `json:"priority"`
		Status          string `json:"status"`
		AssignedAgentID string `json:"assigned_agent_id"`
		CreatedBy       string `json:"created_by"`
		CreatedByType   string `json:"created_by_type"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	issueID := uuid.New().String()
	timestamp := time.Now()

	var assignedAgentID interface{} = nil
	if req.AssignedAgentID != "" {
		assignedAgentID = req.AssignedAgentID
	}

	_, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, description, priority, status,
		                   created_by, created_by_type, assigned_agent_id, queued_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issueID, req.ProjectID, req.Title, req.Description, req.Priority, req.Status,
		req.CreatedBy, req.CreatedByType, assignedAgentID, timestamp)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id": issueID,
	})
}

func issueAPIHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/issues/")
	parts := strings.Split(path, "/")
	issueID := parts[0]

	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case "PUT":
		if len(parts) > 1 && parts[1] == "status" {
			updateIssueStatusHandler(w, r, issueID)
		} else {
			http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		}
	case "DELETE":
		deleteIssueHandler(w, r, issueID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func updateIssueStatusHandler(w http.ResponseWriter, r *http.Request, issueID string) {
	var req struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	_, err := db.Exec(`UPDATE issues SET status = ? WHERE id = ?`, req.Status, issueID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func deleteIssueHandler(w http.ResponseWriter, r *http.Request, issueID string) {
	_, err := db.Exec(`DELETE FROM issues WHERE id = ?`, issueID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

func projectsPageHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	var username, email string
	db.QueryRow(`SELECT name, email FROM users WHERE id = ?`, userID).Scan(&username, &email)

	data := map[string]interface{}{
		"Username": username,
		"Email":    email,
		"UserID":   userID,
	}

	renderTemplate(w, "projects.html", data)
}

func projectsAPIHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		listProjectsHandler(w, r, userID)
	case http.MethodPost:
		createProjectHandler(w, r, userID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func listProjectsHandler(w http.ResponseWriter, r *http.Request, userID string) {
	rows, err := db.Query(`
		SELECT DISTINCT p.id, p.name, p.description, p.owner_id, p.created_at, p.settings,
		       (SELECT COUNT(*) FROM project_members WHERE project_id = p.id) as member_count
		FROM projects p
		LEFT JOIN project_members pm ON p.id = pm.project_id
		WHERE p.owner_id = ? OR pm.user_id = ?
		ORDER BY p.created_at DESC
	`, userID, userID)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	projects := make([]map[string]interface{}, 0)
	for rows.Next() {
		var id, name, ownerID string
		var description sql.NullString
		var createdAt time.Time
		var settings sql.NullString
		var memberCount int

		rows.Scan(&id, &name, &description, &ownerID, &createdAt, &settings, &memberCount)

		project := map[string]interface{}{
			"id":           id,
			"name":         name,
			"description":  description.String,
			"owner_id":     ownerID,
			"created_at":   createdAt,
			"member_count": memberCount,
		}

		if settings.Valid {
			var decoded map[string]interface{}
			if err := json.Unmarshal([]byte(settings.String), &decoded); err == nil {
				project["settings"] = decoded
			}
		}

		projects = append(projects, project)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"projects": projects,
	})
}

func createProjectHandler(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		RepoOption  string `json:"repo_option"`
		RepoURL     string `json:"repo_url"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("Failed to decode request: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("Creating project: name=%s, description=%s, user=%s", req.Name, req.Description, userID)

	if req.Name == "" {
		http.Error(w, "Project name is required", http.StatusBadRequest)
		return
	}

	projectID := uuid.New().String()
	timestamp := time.Now()

	_, err := db.Exec(`
		INSERT INTO projects (id, name, description, owner_id, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, projectID, req.Name, req.Description, userID, timestamp)

	if err != nil {
		log.Printf("Failed to insert project: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	memberID := uuid.New().String()
	_, err = db.Exec(`
		INSERT INTO project_members (id, project_id, user_id, role, joined_at)
		VALUES (?, ?, ?, ?, ?)
	`, memberID, projectID, userID, "owner", timestamp)

	if err != nil {
		log.Printf("Failed to insert project member: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	settings, err := projectfs.SetupProjectWorkspace(projectID, req.RepoOption, req.RepoURL)
	if err != nil {
		log.Printf("workspace: failed to setup workspace for project %s: %v", projectID, err)
		http.Error(w, fmt.Sprintf("Workspace error: %v", err), http.StatusInternalServerError)
		return
	}

	if err := projectfs.SaveSettings(db, projectID, settings); err != nil {
		log.Printf("workspace: failed to save settings for project %s: %v", projectID, err)
	}

	log.Printf("Project created successfully: %s (workspace: %s)", projectID, settings.WorkspacePath)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":            projectID,
		"workspacePath": settings.WorkspacePath,
	})
}

func projectInviteHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	parts := strings.Split(path, "/")
	projectID := parts[0]

	if projectID == "" || len(parts) < 2 || parts[1] != "invite" {
		http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		return
	}

	var ownerID string
	err = db.QueryRow(`SELECT owner_id FROM projects WHERE id = ?`, projectID).Scan(&ownerID)
	if err != nil {
		http.Error(w, "Project not found", http.StatusNotFound)
		return
	}

	if ownerID != userID {
		http.Error(w, "Only project owner can create invites", http.StatusForbidden)
		return
	}

	inviteID := uuid.New().String()
	code := uuid.New().String()[:8]
	timestamp := time.Now()

	log.Printf("Generating invite for project %s: code=%s", projectID, code)

	_, err = db.Exec(`
		INSERT INTO invite_links (id, project_id, code, created_by, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, inviteID, projectID, code, userID, timestamp)

	if err != nil {
		log.Printf("Failed to create invite link: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	log.Printf("Invite created successfully: code=%s", code)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"code": code,
	})
}

func inviteAcceptHandler(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	var userID string
	err = db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusTemporaryRedirect)
		return
	}

	code := strings.TrimPrefix(r.URL.Path, "/invite/")
	if code == "" {
		http.Error(w, "Invalid invite code", http.StatusBadRequest)
		return
	}

	log.Printf("Accepting invite with code: %s", code)

	var projectID, inviteID string
	var uses int
	var maxUses sql.NullInt64
	var expiresAt sql.NullTime

	err = db.QueryRow(`
		SELECT id, project_id, uses, max_uses, expires_at
		FROM invite_links
		WHERE code = ?
	`, code).Scan(&inviteID, &projectID, &uses, &maxUses, &expiresAt)

	if err != nil {
		log.Printf("Failed to find invite: %v", err)
		http.Error(w, "Invalid or expired invite link", http.StatusNotFound)
		return
	}

	log.Printf("Found invite: project=%s, uses=%d, maxUses=%v", projectID, uses, maxUses)

	if expiresAt.Valid && expiresAt.Time.Before(time.Now()) {
		http.Error(w, "Invite link has expired", http.StatusGone)
		return
	}

	if maxUses.Valid && maxUses.Int64 > 0 && int64(uses) >= maxUses.Int64 {
		http.Error(w, "Invite link has reached maximum uses", http.StatusGone)
		return
	}

	var existingMember string
	err = db.QueryRow(`
		SELECT id FROM project_members WHERE project_id = ? AND user_id = ?
	`, projectID, userID).Scan(&existingMember)

	if err == nil {
		http.Redirect(w, r, "/project?id="+projectID, http.StatusSeeOther)
		return
	}

	memberID := uuid.New().String()
	_, err = db.Exec(`
		INSERT INTO project_members (id, project_id, user_id, role, joined_at)
		VALUES (?, ?, ?, ?, ?)
	`, memberID, projectID, userID, "member", time.Now())

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	_, err = db.Exec(`UPDATE invite_links SET uses = uses + 1 WHERE id = ?`, inviteID)
	if err != nil {
		log.Printf("Failed to update invite uses: %v", err)
	}

	http.Redirect(w, r, "/project?id="+projectID, http.StatusSeeOther)
}

func initDatabase() error {
	dbPath := "data/tables.db"
	var err error
	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.Exec(`PRAGMA journal_mode=WAL`)
	db.Exec(`PRAGMA synchronous=NORMAL`)
	db.Exec(`PRAGMA busy_timeout=5000`)

	if err := createTables(); err != nil {
		return fmt.Errorf("failed to create tables: %w", err)
	}

	log.Printf("db: initialized at %s", dbPath)
	return nil
}

func createTables() error {
	tables := []struct {
		name  string
		query string
	}{
		{
			name: "users",
			query: `CREATE TABLE IF NOT EXISTS users (
				id TEXT PRIMARY KEY,
				email TEXT UNIQUE NOT NULL,
				name TEXT NOT NULL,
				avatar TEXT,
				created_at TIMESTAMP NOT NULL
			)`,
		},
		{
			name: "sessions",
			query: `CREATE TABLE IF NOT EXISTS sessions (
				id TEXT PRIMARY KEY,
				user_id TEXT NOT NULL,
				created_at TIMESTAMP NOT NULL,
				FOREIGN KEY (user_id) REFERENCES users(id)
			)`,
		},
		{
			name: "projects",
			query: `CREATE TABLE IF NOT EXISTS projects (
				id TEXT PRIMARY KEY,
				name TEXT NOT NULL,
				description TEXT,
				owner_id TEXT NOT NULL,
				settings TEXT,
				created_at TIMESTAMP NOT NULL,
				FOREIGN KEY (owner_id) REFERENCES users(id)
			)`,
		},
		{
			name: "messages",
			query: `CREATE TABLE IF NOT EXISTS messages (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL,
				sender_id TEXT NOT NULL,
				sender_type TEXT NOT NULL,
				content TEXT NOT NULL,
				message_type TEXT NOT NULL,
				metadata TEXT,
				timestamp TIMESTAMP NOT NULL,
				FOREIGN KEY (project_id) REFERENCES projects(id)
			)`,
		},
		{
			name: "agents",
			query: `CREATE TABLE IF NOT EXISTS agents (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL,
				name TEXT NOT NULL,
				specialization TEXT NOT NULL,
				status TEXT NOT NULL,
				current_task_id TEXT,
				config TEXT,
				created_at TIMESTAMP NOT NULL,
				FOREIGN KEY (project_id) REFERENCES projects(id)
			)`,
		},
		{
			name: "issues",
			query: `CREATE TABLE IF NOT EXISTS issues (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL,
				title TEXT NOT NULL,
				description TEXT,
				priority TEXT NOT NULL,
				status TEXT NOT NULL,
				created_by TEXT NOT NULL,
				created_by_type TEXT NOT NULL,
				assigned_agent_id TEXT,
				queued_at TIMESTAMP,
				started_at TIMESTAMP,
				completed_at TIMESTAMP,
				tags TEXT,
				FOREIGN KEY (project_id) REFERENCES projects(id),
				FOREIGN KEY (assigned_agent_id) REFERENCES agents(id)
			)`,
		},
		{
			name: "artifacts",
			query: `CREATE TABLE IF NOT EXISTS artifacts (
				id TEXT PRIMARY KEY,
				issue_id TEXT NOT NULL,
				type TEXT NOT NULL,
				title TEXT NOT NULL,
				content TEXT NOT NULL,
				language TEXT,
				version INTEGER NOT NULL,
				created_by TEXT NOT NULL,
				approved_by TEXT,
				approved_at TIMESTAMP,
				created_at TIMESTAMP NOT NULL,
				FOREIGN KEY (issue_id) REFERENCES issues(id)
			)`,
		},
		{
			name: "project_members",
			query: `CREATE TABLE IF NOT EXISTS project_members (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL,
				user_id TEXT NOT NULL,
				role TEXT NOT NULL,
				joined_at TIMESTAMP NOT NULL,
				FOREIGN KEY (project_id) REFERENCES projects(id),
				FOREIGN KEY (user_id) REFERENCES users(id),
				UNIQUE(project_id, user_id)
			)`,
		},
		{
			name: "invite_links",
			query: `CREATE TABLE IF NOT EXISTS invite_links (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL,
				code TEXT UNIQUE NOT NULL,
				created_by TEXT NOT NULL,
				expires_at TIMESTAMP,
				max_uses INTEGER,
				uses INTEGER DEFAULT 0,
				created_at TIMESTAMP NOT NULL,
				FOREIGN KEY (project_id) REFERENCES projects(id),
				FOREIGN KEY (created_by) REFERENCES users(id)
			)`,
		},
	}

	for _, tbl := range tables {
		if _, err := db.Exec(tbl.query); err != nil {
			return fmt.Errorf("failed to create table %s: %w", tbl.name, err)
		}
	}

	return nil
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if err := godotenv.Load(); err != nil {
		log.Printf("config: no .env file loaded: %v", err)
	}

	if err := initDatabase(); err != nil {
		log.Fatalf("database initialization failed: %v", err)
	}
	defer db.Close()

	hub := newHub()
	go hub.run()

	mux := http.NewServeMux()

	staticContent, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("failed to create static sub-filesystem: %v", err)
	}
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))

	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/logout", logoutHandler)
	mux.HandleFunc("/projects", projectsPageHandler)
	mux.HandleFunc("/project", projectHandler)
	mux.HandleFunc("/kanban", kanbanHandler)
	mux.HandleFunc("/api/projects", projectsAPIHandler)
	mux.HandleFunc("/api/projects/", projectInviteHandler)
	mux.HandleFunc("/api/issues", issuesAPIHandler)
	mux.HandleFunc("/api/issues/", issueAPIHandler)
	mux.HandleFunc("/invite/", inviteAcceptHandler)
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		wsHandler(w, r, hub)
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("server: listening on :%s", port)
		errCh <- server.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: failed to start: %v", err)
		}
	case <-shutdownCtx.Done():
		log.Println("server: shutdown signal received")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("server: shutdown error: %v", err)
		}
	}

	log.Println("server: stopped")
}
