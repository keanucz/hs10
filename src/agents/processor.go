package agents

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"replychat/src/projectfs"

	"github.com/google/uuid"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type MessageProcessor struct {
	db        *sql.DB
	broadcast chan<- []byte
	aiClient  *openai.Client
}

type AgentResponse struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

type AgentActionPlan struct {
	Files     []GeneratedFile `json:"files"`
	Mutations []FileMutation  `json:"mutations"`
	Notes     []string        `json:"notes"`
}

type GeneratedFile struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	Overwrite *bool  `json:"overwrite,omitempty"`
}

type FileMutation struct {
	Path    string `json:"path"`
	Find    string `json:"find"`
	Replace string `json:"replace"`
}

type structuredBlock struct {
	typeName string
	fields   map[string]string
}

var agentDisplayNames = map[string]string{
	"product_manager":    "Product Manager",
	"backend_architect":  "Backend Architect",
	"frontend_developer": "Frontend Developer",
}

const planFormatInstructions = `Always respond with a minified JSON object describing the work you performed.
Schema: {
  "files": [
    {"path": "relative/path.ext", "content": "full file contents", "overwrite": true}
  ],
  "mutations": [
    {"path": "relative/path.ext", "find": "exact substring to replace", "replace": "new text"}
  ],
  "notes": ["short status strings"]
}
Paths must stay inside the assigned project workspace. Do not wrap JSON in code fences or add commentary.

When you need to collaborate or create workflow artifacts, emit the following structured blocks verbatim (outside of the JSON plan):

@mention - Request collaboration
@mention
agent: Backend Architect
message: Please review the API design
---

@dialog - Request user decisions
@dialog
title: Authentication Method
message: Which method should we use?
options: JWT tokens, OAuth2, Magic links
default: JWT tokens
---

@issue - Create kanban tasks
@issue
title: Implement user authentication system
description: JWT-based auth with login/signup
priority: high
tags: backend, auth, security
assignee: Backend Architect
---`

func ProcessMessage(db *sql.DB, broadcast chan<- []byte, projectID, content, userID string) {
	processor := newMessageProcessor(db, broadcast)
	processor.analyzeAndRespond(projectID, content, userID)
}

func ProcessAgentTask(db *sql.DB, broadcast chan<- []byte, projectID, agentType, issueID, issueTitle, content string) {
	if agentType == "" || content == "" {
		return
	}
	processor := newMessageProcessor(db, broadcast)
	go processor.generateAgentResponse(projectID, agentType, issueID, issueTitle, content)
}

func newMessageProcessor(db *sql.DB, broadcast chan<- []byte) *MessageProcessor {
	apiKey := os.Getenv("OPENAI_API_KEY")
	var client *openai.Client
	if apiKey != "" {
		newClient := openai.NewClient(option.WithAPIKey(apiKey))
		client = &newClient
	}

	return &MessageProcessor{
		db:        db,
		broadcast: broadcast,
		aiClient:  client,
	}
}

func (p *MessageProcessor) analyzeAndRespond(projectID, content, userID string) {
	agent := DetectAgent(content)
	if agent == "" {
		return
	}

	go p.generateAgentResponse(projectID, agent, "", "", content)
}

func (p *MessageProcessor) generateAgentResponse(projectID, agentType, issueID, issueTitle, originalMessage string) {
	var responseText string
	var planNotes []string
	var planForMessage *AgentActionPlan
	var gitResult *projectfs.CommitResult

	workspacePath, workspaceErr := p.ensureWorkspace(projectID)
	if workspaceErr != nil {
		log.Printf("workspace: failed to prepare workspace for project %s: %v", projectID, workspaceErr)
	}

	if p.aiClient != nil {
		systemPrompts := map[string]string{
			"product_manager": fmt.Sprintf(`You are a Product Manager AI agent in a collaborative team workspace.
		Your role is to gather requirements, create user stories, and define project scope.
		Be concise and helpful. Ask clarifying questions when needed.
		Keep responses under 200 words.

		%s`, planFormatInstructions),

			"backend_architect": fmt.Sprintf(`You are a Backend Architect AI agent in a collaborative team workspace.
	Your role is to design APIs, database schemas, and server architecture.
	Be technical but clear. Provide concrete suggestions.
	Keep responses under 200 words.

	%s`, planFormatInstructions),

			"frontend_developer": fmt.Sprintf(`You are a Frontend Developer AI agent in a collaborative team workspace.
	Your role is to build UI components, handle state management, and ensure responsive design.
	Be practical and focus on implementation. Share best practices.
	Keep responses under 200 words.

	%s`, planFormatInstructions),

			"qa_tester": fmt.Sprintf(`You are a QA Tester AI agent in a collaborative team workspace.
	Your role is to validate new functionality, design automated/manual tests, and report regressions.
	Describe the scenarios you verify, add or update test files, and share any defects you find.
	Keep responses under 200 words.

	%s`, planFormatInstructions),

			"devops_engineer": fmt.Sprintf(`You are a DevOps Engineer AI agent in a collaborative team workspace.
	Your role is to manage infrastructure, CI/CD pipelines, deployment scripts, and operational tooling.
	Provide practical improvements, update configs/scripts, and verify commands.
	Keep responses under 200 words.

	%s`, planFormatInstructions),
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		inputMessages := responses.ResponseInputParam{
			responses.ResponseInputItemParamOfMessage(systemPrompts[agentType], responses.EasyInputMessageRoleSystem),
		}

		if workspaceErr == nil && workspacePath != "" {
			inputMessages = append(inputMessages, responses.ResponseInputItemParamOfMessage(
				"Workspace root alias: ./ (project root). Always reference files relative to this root (e.g. src/routes/index.ts). Never mention host-specific paths under data/projects/…",
				responses.EasyInputMessageRoleSystem,
			))
		}

		inputMessages = append(inputMessages, responses.ResponseInputItemParamOfMessage(originalMessage, responses.EasyInputMessageRoleUser))

		resp, err := p.aiClient.Responses.New(ctx, responses.ResponseNewParams{
			Model:           openai.ResponsesModel(openai.ChatModelGPT4oMini),
			Input:           responses.ResponseNewParamsInputUnion{OfInputItemList: inputMessages},
			MaxOutputTokens: openai.Int(1200),
			Temperature:     openai.Float(0.7),
		})

		if err != nil {
			log.Printf("agent: OpenAI API error: %v", err)
			responseText = p.getFallbackResponse(agentType)
		} else if output := resp.OutputText(); output != "" {
			cleanOutput, blocks := extractStructuredBlocks(output)
			if len(blocks) > 0 {
				structuredNotes := p.handleStructuredBlocks(projectID, agentType, blocks)
				planNotes = append(planNotes, structuredNotes...)
			}

			processedOutput := strings.TrimSpace(cleanOutput)
			if processedOutput == "" {
				processedOutput = "Structured actions processed."
			}

			if workspaceErr == nil {
				plan, planErr := parseActionPlan(processedOutput)
				if planErr == nil {
					planCopy := plan
					planForMessage = &planCopy
				}
				if planErr == nil && plan.HasChanges() {
					summary, applyErr := p.applyActionPlan(workspacePath, agentType, plan)
					if applyErr != nil {
						log.Printf("agent: failed to apply plan for project %s: %v", projectID, applyErr)
						responseText = fmt.Sprintf("%s produced changes but hit an error: %v", agentDisplayNames[agentType], applyErr)
					} else {
						responseText = summary
						planNotes = append(planNotes, plan.Notes...)
						commitMsg := buildCommitMessage(agentType, issueTitle, summary, planNotes)
						if commitMsg != "" {
							result, gitErr := projectfs.CommitWorkspaceChanges(workspacePath, commitMsg)
							if gitErr != nil {
								log.Printf("git: commit workflow failed for project %s: %v", projectID, gitErr)
							}
							if result != nil {
								gitResult = result
								if note := gitNote(result, gitErr); note != "" {
									planNotes = append(planNotes, note)
								}
							} else if gitErr != nil {
								planNotes = append(planNotes, fmt.Sprintf("Git commit skipped: %v", gitErr))
							}
						}
					}
				} else {
					responseText = processedOutput
				}
			} else {
				responseText = processedOutput
			}
		} else {
			responseText = p.getFallbackResponse(agentType)
		}
	} else {
		log.Printf("agent: No OpenAI API key configured, using fallback")
		responseText = p.getFallbackResponse(agentType)
	}

	p.sendAgentMessage(projectID, agentType, responseText, "chat", planNotes, workspacePath, planForMessage, gitResult)

	if issueID != "" {
		if err := p.markIssueCompleted(issueID); err != nil {
			log.Printf("agent: failed to complete issue %s: %v", issueID, err)
		}
	}

	if strings.Contains(strings.ToLower(originalMessage), "create task") ||
		strings.Contains(strings.ToLower(originalMessage), "add task") {
		p.proposeTask(projectID, agentType)
	}
}

func (plan AgentActionPlan) HasChanges() bool {
	return len(plan.Files) > 0 || len(plan.Mutations) > 0
}

func (p *MessageProcessor) sendAgentMessage(projectID, agentType, content, messageType string, notes []string, workspacePath string, plan *AgentActionPlan, gitInfo *projectfs.CommitResult) {
	messageID := uuid.New().String()
	timestamp := time.Now()

	metadata := map[string]interface{}{}
	if workspacePath != "" {
		metadata["workspacePath"] = workspacePath
	}
	if len(notes) > 0 {
		metadata["notes"] = notes
	}
	planSummary := summarizePlan(plan)
	if planSummary != nil {
		metadata["plan"] = planSummary
	}
	if gitInfo != nil {
		metadata["git"] = gitInfo
	}
	var metadataPayload map[string]interface{}
	if len(metadata) > 0 {
		metadataPayload = metadata
	}

	_, err := p.db.Exec(`
		INSERT INTO messages (id, project_id, sender_id, sender_type, content, message_type, metadata, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, messageID, projectID, agentType, "agent", content, messageType, marshalEnvelope(metadata), timestamp)
	if err != nil {
		log.Printf("agent: failed to save %s message: %v", messageType, err)
		return
	}

	messagePayload := map[string]interface{}{
		"id":          messageID,
		"projectId":   projectID,
		"senderId":    agentType,
		"senderType":  "agent",
		"senderName":  agentDisplayNames[agentType],
		"content":     content,
		"messageType": messageType,
		"timestamp":   timestamp,
	}
	if workspacePath != "" {
		messagePayload["workspacePath"] = workspacePath
	}
	if len(notes) > 0 {
		messagePayload["notes"] = notes
	}
	if planSummary != nil {
		messagePayload["plan"] = planSummary
	}
	if gitInfo != nil {
		messagePayload["git"] = gitInfo
	}
	if metadataPayload != nil {
		messagePayload["metadata"] = metadataPayload
	}

	if data := marshalEvent("message.received", map[string]interface{}{
		"message": messagePayload,
	}); data != nil {
		p.broadcast <- data
	}
}

func buildCommitMessage(agentType, issueTitle, summary string, notes []string) string {
	candidates := []string{issueTitle}
	if len(notes) > 0 {
		candidates = append(candidates, notes[0])
	}
	candidates = append(candidates, summary)

	var base string
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" {
			base = candidate
			break
		}
	}

	if base == "" {
		base = "Workspace update"
	}

	base = strings.Split(base, "\n")[0]
	if display, ok := agentDisplayNames[agentType]; ok && display != "" {
		return fmt.Sprintf("%s: %s", display, base)
	}
	return base
}

func gitNote(result *projectfs.CommitResult, gitErr error) string {
	if result == nil {
		if gitErr != nil {
			return fmt.Sprintf("Git commit failed: %v", gitErr)
		}
		return ""
	}

	short := shortSHA(result.CommitID)
	branch := strings.TrimSpace(result.Branch)
	if branch == "" {
		branch = "HEAD"
	}

	var status string
	switch {
	case result.Pushed:
		status = fmt.Sprintf("pushed to origin/%s", branch)
	case strings.TrimSpace(result.Remote) != "":
		status = fmt.Sprintf("recorded on %s (push pending)", branch)
	default:
		status = fmt.Sprintf("recorded on %s (no remote)", branch)
	}

	if gitErr != nil {
		status += fmt.Sprintf("; push error: %v", gitErr)
	}

	return fmt.Sprintf("Git commit %s %s", short, status)
}

func shortSHA(commit string) string {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return "unknown"
	}
	runes := []rune(commit)
	if len(runes) > 7 {
		return string(runes[:7])
	}
	return commit
}

func summarizePlan(plan *AgentActionPlan) map[string]interface{} {
	if plan == nil {
		return nil
	}

	files := make([]string, 0, len(plan.Files))
	for _, file := range plan.Files {
		if file.Path != "" {
			files = append(files, file.Path)
		}
	}

	mutations := make([]string, 0, len(plan.Mutations))
	for _, mutation := range plan.Mutations {
		if mutation.Path != "" {
			mutations = append(mutations, mutation.Path)
		}
	}

	if len(files) == 0 && len(mutations) == 0 {
		return nil
	}

	return map[string]interface{}{
		"files":     files,
		"mutations": mutations,
	}
}

func marshalEnvelope(data map[string]interface{}) string {
	if len(data) == 0 {
		return ""
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return ""
	}
	return string(raw)
}

func marshalEvent(eventType string, payload map[string]interface{}) []byte {
	event := AgentResponse{Type: eventType, Payload: payload}
	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("event: failed to marshal %s: %v", eventType, err)
		return nil
	}
	return data
}

func (p *MessageProcessor) ensureWorkspace(projectID string) (string, error) {
	settings, err := projectfs.LoadSettings(p.db, projectID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	workspacePath := settings.WorkspacePath
	if workspacePath == "" {
		workspacePath = projectfs.WorkspacePath(projectID)
	}

	if err := projectfs.EnsureWorkspace(workspacePath); err != nil {
		return "", err
	}

	if settings.WorkspacePath == "" {
		settings.WorkspacePath = workspacePath
		if err := projectfs.SaveSettings(p.db, projectID, settings); err != nil {
			log.Printf("workspace: unable to save default settings for %s: %v", projectID, err)
		}
	}

	return workspacePath, nil
}

func (p *MessageProcessor) applyActionPlan(workspacePath, agentType string, plan AgentActionPlan) (string, error) {
	filesWritten := 0
	mutationsApplied := 0

	for i := range plan.Files {
		file := plan.Files[i]
		cleanPath := normalizePlanPath(workspacePath, file.Path)
		if cleanPath == "" {
			continue
		}
		plan.Files[i].Path = cleanPath

		absPath, err := secureJoin(workspacePath, cleanPath)
		if err != nil {
			return "", err
		}

		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			return "", fmt.Errorf("failed to prepare directory for %s: %w", file.Path, err)
		}

		overwrite := true
		if file.Overwrite != nil {
			overwrite = *file.Overwrite
		}
		if !overwrite {
			if _, err := os.Stat(absPath); err == nil {
				continue
			}
		}

		if err := os.WriteFile(absPath, []byte(file.Content), 0o644); err != nil {
			return "", fmt.Errorf("failed to write file %s: %w", file.Path, err)
		}
		filesWritten++
	}

	for i := range plan.Mutations {
		mutation := plan.Mutations[i]
		if mutation.Path == "" || mutation.Find == "" {
			continue
		}

		cleanPath := normalizePlanPath(workspacePath, mutation.Path)
		if cleanPath == "" {
			continue
		}
		plan.Mutations[i].Path = cleanPath

		absPath, err := secureJoin(workspacePath, cleanPath)
		if err != nil {
			return "", err
		}

		content, err := os.ReadFile(absPath)
		if err != nil {
			return "", fmt.Errorf("failed to read %s for mutation: %w", mutation.Path, err)
		}

		original := string(content)
		if !strings.Contains(original, mutation.Find) {
			continue
		}

		updated := strings.Replace(original, mutation.Find, mutation.Replace, 1)
		if err := os.WriteFile(absPath, []byte(updated), 0o644); err != nil {
			return "", fmt.Errorf("failed to apply mutation to %s: %w", mutation.Path, err)
		}
		mutationsApplied++
	}

	agentName := agentDisplayNames[agentType]
	if agentName == "" {
		agentName = agentType
	}
	summary := fmt.Sprintf("%s updated workspace (files=%d, mutations=%d)", agentName, filesWritten, mutationsApplied)
	if len(plan.Notes) > 0 {
		summary = summary + "; notes: " + strings.Join(plan.Notes, "; ")
	}

	return summary, nil
}

func secureJoin(basePath, relative string) (string, error) {
	cleanBase := filepath.Clean(basePath)
	cleanRel := filepath.Clean(relative)
	joined := filepath.Join(cleanBase, cleanRel)

	if !strings.HasPrefix(joined, cleanBase) {
		return "", fmt.Errorf("path %s escapes workspace", relative)
	}
	return joined, nil
}

func normalizePlanPath(workspacePath, candidate string) string {
	p := strings.TrimSpace(candidate)
	if p == "" {
		return ""
	}

	cleanWorkspace := filepath.Clean(workspacePath)
	cleanCandidate := filepath.Clean(p)

	if strings.HasPrefix(cleanCandidate, cleanWorkspace) {
		if rel, err := filepath.Rel(cleanWorkspace, cleanCandidate); err == nil && rel != "" {
			return rel
		}
	}

	baseSegment := filepath.Base(cleanWorkspace)
	if baseSegment != "" {
		marker := string(os.PathSeparator) + baseSegment + string(os.PathSeparator)
		if idx := strings.Index(cleanCandidate, marker); idx >= 0 {
			trimmed := cleanCandidate[idx+len(marker):]
			if trimmed != "" {
				return trimmed
			}
		}
	}

	cleanCandidate = strings.TrimPrefix(cleanCandidate, string(os.PathSeparator))
	return cleanCandidate
}

func parseActionPlan(output string) (AgentActionPlan, error) {
	clean := strings.TrimSpace(output)
	var plan AgentActionPlan
	if err := json.Unmarshal([]byte(clean), &plan); err == nil {
		return plan, nil
	}

	start := strings.Index(clean, "{")
	end := strings.LastIndex(clean, "}")
	if start >= 0 && end > start {
		candidate := clean[start : end+1]
		if err := json.Unmarshal([]byte(candidate), &plan); err == nil {
			return plan, nil
		}
	}

	return AgentActionPlan{}, fmt.Errorf("unable to parse agent plan output")
}

func (p *MessageProcessor) handleStructuredBlocks(projectID, agentType string, blocks []structuredBlock) []string {
	var notes []string
	for _, block := range blocks {
		switch block.typeName {
		case "issue":
			if note, err := p.handleIssueBlock(projectID, agentType, block.fields); err != nil {
				log.Printf("agent: failed to create issue from block: %v", err)
			} else if note != "" {
				notes = append(notes, note)
			}
		case "mention":
			if note := p.handleMentionBlock(projectID, agentType, block.fields); note != "" {
				notes = append(notes, note)
			}
		case "dialog":
			if note := p.handleDialogBlock(projectID, agentType, block.fields); note != "" {
				notes = append(notes, note)
			}
		}
	}
	return notes
}

func (p *MessageProcessor) handleIssueBlock(projectID, agentType string, fields map[string]string) (string, error) {
	title := fields["title"]
	if title == "" {
		return "", nil
	}

	description := fields["description"]
	priority := normalizePriority(fields["priority"])
	tags := strings.Join(splitCSV(fields["tags"]), ",")
	assigneeID := normalizeAgentIdentifier(fields["assignee"])
	if assigneeID == "" {
		assigneeID = agentType
	}

	var assigned interface{}
	if assigneeID != "" {
		assigned = assigneeID
	}

	issueID := uuid.New().String()
	now := time.Now()

	queueTarget := assigned
	if queueTarget == nil {
		queueTarget = agentType
	}
	_, err := p.db.Exec(`
		INSERT INTO issues (id, project_id, title, description, priority, status,
		                   created_by, created_by_type, assigned_agent_id, queued_agent_id, queued_at, tags, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, issueID, projectID, title, description, priority, "todo", agentType, "agent", assigned, queueTarget, now, tags, now)
	if err != nil {
		return "", err
	}

	issuePayload := map[string]interface{}{
		"id":          issueID,
		"projectId":   projectID,
		"title":       title,
		"description": description,
		"priority":    priority,
		"status":      "todo",
		"createdBy":   agentType,
		"assignee":    assigneeID,
	}

	p.broadcast <- marshalEvent("issue.created", map[string]interface{}{
		"issue":            issuePayload,
		"requiresApproval": false,
	})

	return fmt.Sprintf("Created issue: %s", title), nil
}

func (p *MessageProcessor) handleMentionBlock(projectID, agentType string, fields map[string]string) string {
	message := fields["message"]
	if message == "" {
		return ""
	}
	target := fields["agent"]
	if target == "" {
		target = "team"
	}
	content := fmt.Sprintf("@mention to %s: %s", target, message)
	p.sendAgentMessage(projectID, agentType, content, "system", nil, "", nil, nil)
	return fmt.Sprintf("Mentioned %s", target)
}

func (p *MessageProcessor) markIssueCompleted(issueID string) error {
	now := time.Now()
	res, err := p.db.Exec(`
		UPDATE issues
		SET status = 'done',
		    completed_at = COALESCE(completed_at, ?),
		    queued_agent_id = NULL
		WHERE id = ? AND status != 'done'
	`, now, issueID)
	if err != nil {
		return err
	}

	if rows, _ := res.RowsAffected(); rows == 0 {
		return nil
	}

	issue, err := fetchIssueForBroadcast(p.db, issueID)
	if err != nil {
		return err
	}

	if data := marshalEvent("issue.updated", map[string]interface{}{
		"issue": issue,
	}); data != nil {
		p.broadcast <- data
	}

	return nil
}

func fetchIssueForBroadcast(db *sql.DB, issueID string) (map[string]interface{}, error) {
	row := db.QueryRow(`
		SELECT id, project_id, title, description, priority, status,
		       created_by, created_by_type, assigned_agent_id, queued_agent_id,
		       queued_at, started_at, completed_at, created_at
		FROM issues
		WHERE id = ?
	`, issueID)

	var (
		id, projectID, title, description, priority, status, createdBy, createdByType string
		assignedAgentID, queuedAgentID                                                sql.NullString
		queuedAt, startedAt, completedAt, createdAt                                   sql.NullTime
	)

	if err := row.Scan(&id, &projectID, &title, &description, &priority, &status,
		&createdBy, &createdByType, &assignedAgentID, &queuedAgentID,
		&queuedAt, &startedAt, &completedAt, &createdAt); err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":              id,
		"projectId":       projectID,
		"title":           title,
		"description":     description,
		"priority":        priority,
		"status":          status,
		"createdBy":       createdBy,
		"createdByType":   createdByType,
		"assignedAgentId": assignedAgentID.String,
		"queuedAgentId":   queuedAgentID.String,
		"queuedAt":        queuedAt.Time,
		"startedAt":       startedAt.Time,
		"completedAt":     completedAt.Time,
		"createdAt":       createdAt.Time,
	}, nil
}

func (p *MessageProcessor) handleDialogBlock(projectID, agentType string, fields map[string]string) string {
	dialogID := uuid.New().String()
	options := splitCSV(fields["options"])
	optionJSON, _ := json.Marshal(options)
	now := time.Now()
	issueID := fields["issue"]

	_, err := p.db.Exec(`
		INSERT INTO dialogs (id, project_id, agent_id, issue_id, title, message, options, default_option, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, dialogID, projectID, agentType, issueID, fields["title"], fields["message"], string(optionJSON), fields["default"], "open", now)
	if err != nil {
		log.Printf("dialog: failed to persist dialog %s: %v", dialogID, err)
	}
	dialog := map[string]interface{}{
		"id":            dialogID,
		"projectId":     projectID,
		"agentId":       agentType,
		"title":         fields["title"],
		"message":       fields["message"],
		"options":       options,
		"defaultOption": fields["default"],
		"status":        "open",
		"issueId":       issueID,
	}

	if data := marshalEvent("dialog.requested", map[string]interface{}{
		"dialog":  dialog,
		"agentId": agentType,
	}); data != nil {
		p.broadcast <- data
	}

	if dialog["title"] != "" {
		return fmt.Sprintf("Requested decision: %s", dialog["title"])
	}
	return "Requested user decision"
}

func extractStructuredBlocks(text string) (string, []structuredBlock) {
	lines := strings.Split(text, "\n")
	var blocks []structuredBlock
	var outputLines []string

	for i := 0; i < len(lines); {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "@") {
			typeName := strings.TrimPrefix(trimmed, "@")
			i++
			start := i
			for i < len(lines) && strings.TrimSpace(lines[i]) != "---" {
				i++
			}
			if i >= len(lines) {
				outputLines = append(outputLines, lines[start-1:]...)
				break
			}
			blockLines := lines[start:i]
			blocks = append(blocks, structuredBlock{
				typeName: strings.ToLower(strings.TrimSpace(typeName)),
				fields:   parseBlockFields(blockLines),
			})
			i++ // skip ---
			continue
		}
		outputLines = append(outputLines, lines[i])
		i++
	}

	clean := strings.TrimSpace(strings.Join(outputLines, "\n"))
	return clean, blocks
}

func parseBlockFields(lines []string) map[string]string {
	fields := make(map[string]string)
	var currentKey string
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			key := strings.ToLower(strings.TrimSpace(parts[0]))
			value := strings.TrimSpace(parts[1])
			fields[key] = value
			currentKey = key
		} else if currentKey != "" {
			fields[currentKey] = strings.TrimSpace(fields[currentKey] + " " + line)
		}
	}
	return fields
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizePriority(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "urgent":
		return "urgent"
	case "high":
		return "high"
	case "low":
		return "low"
	default:
		return "medium"
	}
}

func normalizeAgentIdentifier(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	v = strings.ReplaceAll(v, "-", "_")
	v = strings.ReplaceAll(v, " ", "_")
	switch v {
	case "pm", "productmanager", "product_manager":
		return "product_manager"
	case "backend", "backend_architect", "backendarchitect", "ba":
		return "backend_architect"
	case "frontend", "frontend_developer", "frontenddeveloper", "fd":
		return "frontend_developer"
	case "qa", "tester", "qa_tester", "qatester":
		return "qa_tester"
	case "devops", "devops_engineer", "devopsengineer", "sre":
		return "devops_engineer"
	default:
		return ""
	}
}

func (p *MessageProcessor) getFallbackResponse(agentType string) string {
	responses := map[string]string{
		"product_manager": `I understand you need help with requirements. Let me analyze what you're asking for.

Based on your message, I can help break this down into actionable tasks. Would you like me to create some initial user stories and features?`,

		"backend_architect": `I can help with the backend architecture for this feature.

Here's what I'm thinking:
- Design the database schema
- Create REST API endpoints
- Set up proper error handling and validation

Should I create tasks for these items?`,

		"frontend_developer": `I can help build the frontend components for this.

I'll focus on:
- Creating reusable UI components
- Implementing responsive design
- Ensuring good UX patterns

Let me know if you'd like me to start on any specific part.`,

		"qa_tester": `I can validate the functionality we just discussed.

I'll prepare or update test cases, run the relevant suites, and report any regressions I find.
Let me know if there are specific scenarios or environments I should focus on.`,

		"devops_engineer": `I can help with the infrastructure and delivery pipeline for this work.

I’m thinking:
- Update CI/CD or deployment scripts
- Adjust infrastructure-as-code templates
- Verify monitoring or rollout steps

Would you like me to start on any particular environment or pipeline stage?`,
	}

	if response, ok := responses[agentType]; ok {
		return response
	}
	return "I'm ready to help with this task."
}

func (p *MessageProcessor) proposeTask(projectID, agentType string) {
	time.Sleep(1 * time.Second)

	taskID := uuid.New().String()
	timestamp := time.Now()

	taskTitles := map[string]string{
		"product_manager":    "Define user requirements and acceptance criteria",
		"backend_architect":  "Design API endpoints and database schema",
		"frontend_developer": "Create responsive UI components",
		"qa_tester":          "Validate latest feature and regression suite",
		"devops_engineer":    "Improve deployment pipeline and infrastructure",
	}

	taskDescriptions := map[string]string{
		"product_manager":    "Gather and document user requirements, create user stories with clear acceptance criteria",
		"backend_architect":  "Design RESTful API structure and database schema with proper relationships",
		"frontend_developer": "Build reusable React components with responsive design and accessibility",
		"qa_tester":          "Design automated/manual tests for new features, run regression suites, and report issues",
		"devops_engineer":    "Update CI/CD configuration, infrastructure-as-code, or deployment scripts to support new changes",
	}

	_, err := p.db.Exec(`
		INSERT INTO issues (id, project_id, title, description, priority, status,
		created_by, created_by_type, assigned_agent_id, queued_agent_id, queued_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, NULL, ?)
	`, taskID, projectID, taskTitles[agentType], taskDescriptions[agentType], "medium", "proposed", agentType, "agent", timestamp)

	if err != nil {
		log.Printf("agent: failed to create task: %v", err)
		return
	}

	if data := marshalEvent("issue.created", map[string]interface{}{
		"issue": map[string]interface{}{
			"id":          taskID,
			"projectId":   projectID,
			"title":       taskTitles[agentType],
			"description": taskDescriptions[agentType],
			"priority":    "medium",
			"status":      "proposed",
			"createdBy":   agentType,
		},
		"requiresApproval": true,
	}); data != nil {
		p.broadcast <- data
	}
}
