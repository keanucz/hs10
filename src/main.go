package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
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
var globalHub *Hub

var defaultAgents = []string{"product_manager", "backend_architect", "frontend_developer"}

func currentUserID(r *http.Request) (string, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil {
		return "", err
	}

	var userID string
	if err := db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID); err != nil {
		return "", err
	}
	return userID, nil
}

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

func sendSystemMessage(projectID, content string) {
	if projectID == "" || content == "" {
		return
	}

	messageID := uuid.New().String()
	timestamp := time.Now()

	_, err := db.Exec(`
		INSERT INTO messages (id, project_id, sender_id, sender_type, content, message_type, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, messageID, projectID, "system", "system", content, "system", timestamp)
	if err != nil {
		log.Printf("system message: failed to save: %v", err)
		return
	}

	if globalHub == nil {
		return
	}

	response := map[string]interface{}{
		"type": "message.received",
		"payload": map[string]interface{}{
			"message": map[string]interface{}{
				"id":          messageID,
				"projectId":   projectID,
				"senderId":    "system",
				"senderType":  "system",
				"senderName":  "System",
				"content":     content,
				"messageType": "system",
				"timestamp":   timestamp,
			},
		},
	}

	if data, err := json.Marshal(response); err == nil {
		globalHub.broadcast <- data
	}
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
		       queued_agent_id, queued_at, started_at, completed_at, created_at
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
		var assignedAgentID, queuedAgentID sql.NullString
		var queuedAt, startedAt, completedAt, createdAt sql.NullTime

		rows.Scan(&id, &title, &description, &priority, &status, &createdBy, &createdByType,
			&assignedAgentID, &queuedAgentID, &queuedAt, &startedAt, &completedAt, &createdAt)

		issue := map[string]interface{}{
			"id":                id,
			"title":             title,
			"description":       description,
			"priority":          priority,
			"status":            status,
			"created_by":        createdBy,
			"created_by_type":   createdByType,
			"assigned_agent_id": assignedAgentID.String,
			"queued_agent_id":   queuedAgentID.String,
			"queued_at":         queuedAt.Time,
			"started_at":        startedAt.Time,
			"completed_at":      completedAt.Time,
			"created_at":        createdAt.Time,
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

	if req.ProjectID == "" {
		req.ProjectID = "default"
	}
	if req.Status == "" {
		req.Status = "proposed"
	}

	agentID := determineIssueAgent(req.AssignedAgentID, req.Title, req.Description)
	var assignedAgent interface{}
	if agentID != "" {
		assignedAgent = agentID
	}

	issueID := uuid.New().String()
	createdAt := time.Now()
	_, err := db.Exec(`
		INSERT INTO issues (id, project_id, title, description, priority, status,
		                   created_by, created_by_type, assigned_agent_id, queued_agent_id, queued_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?)
	`, issueID, req.ProjectID, req.Title, req.Description, req.Priority, req.Status,
		req.CreatedBy, req.CreatedByType, assignedAgent, createdAt)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if req.Status == "todo" && agentID != "" {
		if err := queueIssue(issueID, agentID); err != nil {
			log.Printf("issue: failed to queue %s: %v", issueID, err)
		}
		pushAgentStatusUpdate(req.ProjectID)
	}

	broadcastIssueChange(issueID)

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

	if req.Status == "" {
		http.Error(w, "status is required", http.StatusBadRequest)
		return
	}

	var (
		projectID, title, description string
		assignedAgentID               sql.NullString
	)

	row := db.QueryRow(`SELECT project_id, title, description, assigned_agent_id FROM issues WHERE id = ?`, issueID)
	if err := row.Scan(&projectID, &title, &description, &assignedAgentID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Error(w, "issue not found", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	updateFields := []string{"status = ?"}
	args := []interface{}{req.Status}
	now := time.Now()

	if req.Status != "todo" {
		updateFields = append(updateFields, "queued_agent_id = NULL")
	}

	switch req.Status {
	case "inProgress":
		updateFields = append(updateFields, "started_at = COALESCE(started_at, ?)")
		args = append(args, now)
	case "done":
		updateFields = append(updateFields, "completed_at = COALESCE(completed_at, ?)")
		args = append(args, now)
	}

	args = append(args, issueID)
	query := fmt.Sprintf("UPDATE issues SET %s WHERE id = ?", strings.Join(updateFields, ", "))
	if _, err := db.Exec(query, args...); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	agentID := assignedAgentID.String
	if req.Status == "todo" {
		if agentID == "" {
			agentID = determineIssueAgent("", title, description)
			if agentID != "" {
				if _, err := db.Exec(`UPDATE issues SET assigned_agent_id = ? WHERE id = ?`, agentID, issueID); err != nil {
					log.Printf("issue: failed to assign agent for %s: %v", issueID, err)
				}
			}
		}

		if err := queueIssue(issueID, agentID); err != nil {
			log.Printf("issue: failed to queue %s: %v", issueID, err)
		}
	}

	broadcastIssueChange(issueID)
	pushAgentStatusUpdate(projectID)
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

func respondToDialog(dialogID, userID, selected string) (map[string]interface{}, error) {
	var (
		projectID, agentID, title, message, status, defaultOption string
		optionsJSON, issueID, respondedBy                         sql.NullString
		respondedAt, createdAt                                    sql.NullTime
	)

	row := db.QueryRow(`
		SELECT project_id, agent_id, title, message, status, default_option, options,
		       issue_id, responded_by, responded_at, created_at
		FROM dialogs
		WHERE id = ?
	`, dialogID)

	if err := row.Scan(&projectID, &agentID, &title, &message, &status, &defaultOption, &optionsJSON,
		&issueID, &respondedBy, &respondedAt, &createdAt); err != nil {
		return nil, err
	}
	if status != "open" {
		return nil, fmt.Errorf("dialog already resolved")
	}

	var options []string
	if optionsJSON.Valid {
		_ = json.Unmarshal([]byte(optionsJSON.String), &options)
	}

	selectedOption := strings.TrimSpace(selected)
	if selectedOption == "" {
		selectedOption = defaultOption
	}
	if selectedOption == "" && len(options) > 0 {
		selectedOption = options[0]
	}
	if selectedOption == "" {
		return nil, fmt.Errorf("selected option required")
	}

	if len(options) > 0 {
		valid := false
		for _, opt := range options {
			trimmed := strings.TrimSpace(opt)
			if strings.EqualFold(trimmed, strings.TrimSpace(selectedOption)) {
				selectedOption = trimmed
				valid = true
				break
			}
		}
		if !valid {
			if defaultOption != "" {
				selectedOption = defaultOption
				valid = true
			} else {
				return nil, fmt.Errorf("invalid option selected")
			}
		}
	}

	now := time.Now()
	_, err := db.Exec(`
		UPDATE dialogs
		SET status = 'resolved', selected_option = ?, responded_by = ?, responded_at = ?
		WHERE id = ? AND status = 'open'
	`, selectedOption, userID, now, dialogID)
	if err != nil {
		return nil, err
	}

	response := map[string]interface{}{
		"id":             dialogID,
		"projectId":      projectID,
		"agentId":        agentID,
		"title":          title,
		"message":        message,
		"selectedOption": selectedOption,
		"respondedBy":    userID,
		"respondedAt":    now,
		"issueId":        issueID.String,
	}

	event := agents.AgentResponse{
		Type: "dialog.responded",
		Payload: map[string]interface{}{
			"dialog": response,
		},
	}
	if globalHub != nil {
		if data, err := json.Marshal(event); err == nil {
			globalHub.broadcast <- data
		}
	}

	userName := lookupUserName(userID)
	if userName == "" {
		userName = "A teammate"
	}
	response["respondedByName"] = userName
	summary := fmt.Sprintf("%s selected '%s' for dialog '%s'.", userName, selectedOption, title)
	sendSystemMessage(projectID, summary)

	return response, nil
}

func dialogsAPIHandler(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		projectID = "default"
	}

	rows, err := db.Query(`
		SELECT id, project_id, agent_id, issue_id, title, message, options, default_option,
		       status, selected_option, responded_by, responded_at, created_at
		FROM dialogs
		WHERE project_id = ?
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	dialogs := make([]map[string]interface{}, 0)
	for rows.Next() {
		var (
			id, projID, agentID, issueID, title, message, optionsJSON, defaultOption, status, selectedOption, respondedBy string
			respondedAt, createdAt                                                                                        sql.NullTime
		)
		if err := rows.Scan(&id, &projID, &agentID, &issueID, &title, &message, &optionsJSON, &defaultOption,
			&status, &selectedOption, &respondedBy, &respondedAt, &createdAt); err != nil {
			continue
		}
		var opts []string
		_ = json.Unmarshal([]byte(optionsJSON), &opts)
		dialog := map[string]interface{}{
			"id":             id,
			"projectId":      projID,
			"agentId":        agentID,
			"issueId":        issueID,
			"title":          title,
			"message":        message,
			"options":        opts,
			"defaultOption":  defaultOption,
			"status":         status,
			"selectedOption": selectedOption,
			"respondedBy":    respondedBy,
			"respondedAt":    respondedAt.Time,
			"createdAt":      createdAt.Time,
		}
		dialogs = append(dialogs, dialog)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"dialogs": dialogs,
	})
}

func dialogActionHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/dialogs/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Dialog ID required", http.StatusBadRequest)
		return
	}
	DialogID := parts[0]

	if len(parts) > 1 && parts[1] == "respond" {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		userID, err := currentUserID(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		var req struct {
			SelectedOption string `json:"selected_option"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		resp, err := respondToDialog(DialogID, userID, req.SelectedOption)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	http.Error(w, "Invalid dialog endpoint", http.StatusBadRequest)
}

func agentQueuesAPIHandler(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		projectID = "default"
	}

	stats, err := collectQueueStatsForProject(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"queues": stats,
	})
}

func agentStatusAPIHandler(w http.ResponseWriter, r *http.Request) {
	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		projectID = "default"
	}

	stats, err := collectQueueStatsForProject(projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"statuses": stats,
	})
}

type AgentQueueStat struct {
	ProjectID         string `json:"project_id"`
	AgentID           string `json:"agent_id"`
	QueueDepth        int    `json:"queue_depth"`
	InProgress        int    `json:"in_progress"`
	Status            string `json:"status"`
	CurrentIssueID    string `json:"current_issue_id,omitempty"`
	CurrentIssueTitle string `json:"current_issue_title,omitempty"`
}

type queuedIssue struct {
	ID          string
	ProjectID   string
	AgentID     string
	Title       string
	Description string
	Priority    string
}

func determineIssueAgent(requestedAgent, title, description string) string {
	if requestedAgent != "" {
		return requestedAgent
	}

	content := strings.TrimSpace(fmt.Sprintf("%s %s", title, description))
	if content == "" {
		return ""
	}

	return agents.DetectAgent(content)
}

func queueIssue(issueID, agentID string) error {
	if agentID == "" {
		return nil
	}

	now := time.Now()
	_, err := db.Exec(`UPDATE issues SET queued_agent_id = ?, queued_at = ? WHERE id = ?`, agentID, now, issueID)
	return err
}

func claimNextQueuedIssue() (*queuedIssue, error) {
	row := db.QueryRow(`
		SELECT id, project_id, queued_agent_id, title, description, priority
		FROM issues
		WHERE status = 'todo' AND queued_agent_id IS NOT NULL
		ORDER BY
			CASE priority
				WHEN 'urgent' THEN 0
				WHEN 'high' THEN 1
				WHEN 'medium' THEN 2
				ELSE 3
			END,
			queued_at ASC
		LIMIT 1
	`)

	var issue queuedIssue
	if err := row.Scan(&issue.ID, &issue.ProjectID, &issue.AgentID, &issue.Title, &issue.Description, &issue.Priority); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	now := time.Now()
	res, err := db.Exec(`
		UPDATE issues
		SET status = 'inProgress',
			started_at = COALESCE(started_at, ?),
			assigned_agent_id = COALESCE(assigned_agent_id, queued_agent_id),
			queued_agent_id = NULL
		WHERE id = ? AND status = 'todo'
	`, now, issue.ID)
	if err != nil {
		return nil, err
	}

	rowsAffected, _ := res.RowsAffected()
	if rowsAffected == 0 {
		return nil, nil
	}

	return &issue, nil
}

func fetchIssue(issueID string) (map[string]interface{}, error) {
	var (
		id, projectID, title, description, priority, status, createdBy, createdByType string
		assignedAgentID, queuedAgentID                                                sql.NullString
		queuedAt, startedAt, completedAt, createdAt                                   sql.NullTime
	)

	row := db.QueryRow(`
		SELECT id, project_id, title, description, priority, status,
		       created_by, created_by_type, assigned_agent_id, queued_agent_id,
		       queued_at, started_at, completed_at, created_at
		FROM issues
		WHERE id = ?
	`, issueID)

	if err := row.Scan(&id, &projectID, &title, &description, &priority, &status,
		&createdBy, &createdByType, &assignedAgentID, &queuedAgentID,
		&queuedAt, &startedAt, &completedAt, &createdAt); err != nil {
		return nil, err
	}

	issue := map[string]interface{}{
		"id":                id,
		"project_id":        projectID,
		"title":             title,
		"description":       description,
		"priority":          priority,
		"status":            status,
		"created_by":        createdBy,
		"created_by_type":   createdByType,
		"assigned_agent_id": assignedAgentID.String,
		"queued_agent_id":   queuedAgentID.String,
		"queued_at":         queuedAt.Time,
		"started_at":        startedAt.Time,
		"completed_at":      completedAt.Time,
		"created_at":        createdAt.Time,
	}

	return issue, nil
}

func broadcastIssueChange(issueID string) {
	if globalHub == nil {
		return
	}

	issue, err := fetchIssue(issueID)
	if err != nil {
		log.Printf("issue: unable to broadcast update for %s: %v", issueID, err)
		return
	}

	event := map[string]interface{}{
		"type": "issue.updated",
		"payload": map[string]interface{}{
			"issue": issue,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("issue: failed to marshal update event: %v", err)
		return
	}

	select {
	case globalHub.broadcast <- data:
	default:
		log.Print("issue: broadcast channel full, dropping update event")
	}
}

func collectQueueStatsForProject(projectID string) ([]AgentQueueStat, error) {
	stats := make(map[string]*AgentQueueStat)
	ensureEntry := func(agentID string) *AgentQueueStat {
		if entry, ok := stats[agentID]; ok {
			return entry
		}
		entry := &AgentQueueStat{ProjectID: projectID, AgentID: agentID}
		stats[agentID] = entry
		return entry
	}

	for _, agentID := range defaultAgents {
		ensureEntry(agentID)
	}

	queueRows, err := db.Query(`
		SELECT queued_agent_id, COUNT(*)
		FROM issues
		WHERE project_id = ? AND status = 'todo' AND queued_agent_id IS NOT NULL
		GROUP BY queued_agent_id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer queueRows.Close()

	for queueRows.Next() {
		var agentID string
		var count int
		if err := queueRows.Scan(&agentID, &count); err != nil {
			return nil, err
		}
		if agentID == "" {
			continue
		}
		entry := ensureEntry(agentID)
		entry.QueueDepth = count
	}

	inProgressRows, err := db.Query(`
		SELECT assigned_agent_id, COUNT(*)
		FROM issues
		WHERE project_id = ? AND status = 'inProgress' AND assigned_agent_id IS NOT NULL
		GROUP BY assigned_agent_id
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer inProgressRows.Close()

	for inProgressRows.Next() {
		var agentID string
		var count int
		if err := inProgressRows.Scan(&agentID, &count); err != nil {
			return nil, err
		}
		if agentID == "" {
			continue
		}
		entry := ensureEntry(agentID)
		entry.InProgress = count
	}

	issueRows, err := db.Query(`
		SELECT id, title, assigned_agent_id
		FROM issues
		WHERE project_id = ? AND status = 'inProgress' AND assigned_agent_id IS NOT NULL
		ORDER BY started_at ASC
	`, projectID)
	if err == nil {
		defer issueRows.Close()
		for issueRows.Next() {
			var issueID, title, agentID string
			if err := issueRows.Scan(&issueID, &title, &agentID); err != nil {
				continue
			}
			entry := ensureEntry(agentID)
			if entry.CurrentIssueID == "" {
				entry.CurrentIssueID = issueID
				entry.CurrentIssueTitle = title
			}
		}
	}

	result := make([]AgentQueueStat, 0, len(stats))
	for _, agentID := range defaultAgents {
		if entry, ok := stats[agentID]; ok {
			entry.Status = deriveAgentStatus(entry)
			result = append(result, *entry)
			delete(stats, agentID)
		}
	}

	for _, entry := range stats {
		entry.Status = deriveAgentStatus(entry)
		result = append(result, *entry)
	}

	return result, nil
}

func deriveAgentStatus(stat *AgentQueueStat) string {
	switch {
	case stat.InProgress > 0:
		return "working"
	case stat.QueueDepth > 0:
		return "queued"
	default:
		return "idle"
	}
}

func broadcastAgentQueueSnapshot(hub *Hub, projectID string, stats []AgentQueueStat) {
	if hub == nil {
		return
	}

	event := map[string]interface{}{
		"type": "agent.queue",
		"payload": map[string]interface{}{
			"projectId": projectID,
			"queues":    stats,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("queue: failed to marshal queue snapshot: %v", err)
		return
	}

	select {
	case hub.broadcast <- data:
	default:
		log.Printf("queue: dropping snapshot broadcast for project %s", projectID)
	}
}

func broadcastAgentStatusSnapshot(hub *Hub, projectID string, stats []AgentQueueStat) {
	if hub == nil {
		return
	}

	event := map[string]interface{}{
		"type": "agent.status",
		"payload": map[string]interface{}{
			"projectId": projectID,
			"statuses":  stats,
		},
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("agent status: failed to marshal snapshot: %v", err)
		return
	}

	select {
	case hub.broadcast <- data:
	default:
		log.Printf("agent status: dropping snapshot for project %s", projectID)
	}
}

func pushAgentStatusUpdate(projectID string) {
	if globalHub == nil || projectID == "" {
		return
	}
	stats, err := collectQueueStatsForProject(projectID)
	if err != nil {
		log.Printf("agent status: failed to collect stats for %s: %v", projectID, err)
		return
	}
	broadcastAgentQueueSnapshot(globalHub, projectID, stats)
	broadcastAgentStatusSnapshot(globalHub, projectID, stats)
}

func listProjectIDs() ([]string, error) {
	rows, err := db.Query(`SELECT id FROM projects`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		ids = append(ids, "default")
	}
	return ids, nil
}

func startQueueWorker(ctx context.Context, hub *Hub, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("queue: worker shutting down")
			return
		case <-ticker.C:
			projects, err := listProjectIDs()
			if err != nil {
				log.Printf("queue: failed to list projects: %v", err)
				continue
			}

			for _, projectID := range projects {
				stats, err := collectQueueStatsForProject(projectID)
				if err != nil {
					log.Printf("queue: failed to collect stats for %s: %v", projectID, err)
					continue
				}
				broadcastAgentQueueSnapshot(hub, projectID, stats)
				broadcastAgentStatusSnapshot(hub, projectID, stats)
			}
		}
	}
}

func startTaskProcessor(ctx context.Context, broadcast chan<- []byte, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("tasks: processor shutting down")
			return
		case <-ticker.C:
			issue, err := claimNextQueuedIssue()
			if err != nil {
				log.Printf("tasks: failed to claim issue: %v", err)
				continue
			}
			if issue == nil {
				continue
			}

			prompt := buildAgentTaskPrompt(issue)
			agents.ProcessAgentTask(db, broadcast, issue.ProjectID, issue.AgentID, prompt)
			broadcastIssueChange(issue.ID)
			pushAgentStatusUpdate(issue.ProjectID)
		}
	}
}

func lookupUserName(userID string) string {
	if userID == "" {
		return ""
	}
	var name string
	if err := db.QueryRow(`SELECT name FROM users WHERE id = ?`, userID).Scan(&name); err != nil {
		return ""
	}
	return name
}

func buildAgentTaskPrompt(issue *queuedIssue) string {
	desc := strings.TrimSpace(issue.Description)
	if desc == "" {
		desc = "No additional description provided."
	}

	priority := strings.ToUpper(issue.Priority)
	return fmt.Sprintf(`You have been assigned a queued task.

Title: %s
Priority: %s
Description:
%s

Begin work immediately, update the project workspace as needed, and summarize your changes when you respond.`, issue.Title, priority, desc)
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
				queued_agent_id TEXT,
				queued_at TIMESTAMP,
				started_at TIMESTAMP,
				completed_at TIMESTAMP,
				tags TEXT,
				created_at TIMESTAMP NOT NULL,
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
		{
			name: "dialogs",
			query: `CREATE TABLE IF NOT EXISTS dialogs (
				id TEXT PRIMARY KEY,
				project_id TEXT NOT NULL,
				agent_id TEXT NOT NULL,
				issue_id TEXT,
				title TEXT,
				message TEXT,
				options TEXT,
				default_option TEXT,
				status TEXT NOT NULL,
				selected_option TEXT,
				responded_by TEXT,
				responded_at TIMESTAMP,
				created_at TIMESTAMP NOT NULL,
				FOREIGN KEY (project_id) REFERENCES projects(id)
			)`,
		},
	}

	for _, tbl := range tables {
		if _, err := db.Exec(tbl.query); err != nil {
			return fmt.Errorf("failed to create table %s: %w", tbl.name, err)
		}
	}

	return ensureIssueColumns()
}

func ensureIssueColumns() error {
	rows, err := db.Query(`PRAGMA table_info(issues)`)
	if err != nil {
		return fmt.Errorf("failed to inspect issues table: %w", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultVal interface{}
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("failed to scan issues columns: %w", err)
		}
		columns[name] = true
	}

	if !columns["queued_agent_id"] {
		if _, err := db.Exec(`ALTER TABLE issues ADD COLUMN queued_agent_id TEXT`); err != nil {
			return fmt.Errorf("failed to add queued_agent_id column: %w", err)
		}
	}

	if !columns["created_at"] {
		if _, err := db.Exec(`ALTER TABLE issues ADD COLUMN created_at TIMESTAMP`); err != nil {
			return fmt.Errorf("failed to add created_at column: %w", err)
		}
		if _, err := db.Exec(`UPDATE issues SET created_at = COALESCE(queued_at, CURRENT_TIMESTAMP) WHERE created_at IS NULL`); err != nil {
			return fmt.Errorf("failed to backfill created_at column: %w", err)
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
	globalHub = hub
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
	mux.HandleFunc("/api/dialogs", dialogsAPIHandler)
	mux.HandleFunc("/api/dialogs/", dialogActionHandler)
	mux.HandleFunc("/api/agent-queues", agentQueuesAPIHandler)
	mux.HandleFunc("/api/agent-status", agentStatusAPIHandler)
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

	go startQueueWorker(shutdownCtx, hub, 5*time.Second)
	go startTaskProcessor(shutdownCtx, hub.broadcast, 4*time.Second)

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
