# Future Development Roadmap

Plans for extending ReplyChat into a full AI agent collaboration platform.

## Table of Contents
1. [Kanban Board with Agent Task Management](#kanban-board-with-agent-task-management)
2. [Message Persistence](#message-persistence)
3. [Agent File Editing System](#agent-file-editing-system)
4. [Project Isolation](#project-isolation)
5. [Agent Context & Memory](#agent-context--memory)

---

## Kanban Board with Agent Task Management

### Vision
Agents can create tasks that automatically get picked up and executed by appropriate agents, creating a fully autonomous development workflow.

### Current State
- ✅ Database schema exists (issues table)
- ✅ Agents can propose tasks via @issue blocks
- ❌ No Kanban UI
- ❌ No automatic task pickup
- ❌ No task queue management

### Implementation Plan

#### Phase 1: Kanban Board UI (2-3 hours)

**Frontend Components:**

```javascript
// Kanban board component structure
const KanbanBoard = {
    columns: [
        { id: 'proposed', name: 'Proposed', color: 'yellow' },
        { id: 'todo', name: 'To Do', color: 'blue' },
        { id: 'inProgress', name: 'In Progress', color: 'purple' },
        { id: 'review', name: 'Review', color: 'orange' },
        { id: 'done', name: 'Done', color: 'green' }
    ]
}
```

**Features:**
- Drag-and-drop between columns
- Task cards with: title, description, assigned agent, priority
- Filter by agent, priority, tags
- Quick actions: approve, assign, delete
- Real-time updates via WebSocket

**Routes:**
```go
// Add to main.go
mux.HandleFunc("/api/issues", listIssuesHandler)
mux.HandleFunc("/api/issues/create", createIssueHandler)
mux.HandleFunc("/api/issues/{id}/update", updateIssueHandler)
mux.HandleFunc("/api/issues/{id}/assign", assignIssueHandler)
```

**WebSocket Events:**
```javascript
// New WebSocket message types
{
    type: "issue.updated",
    payload: { issue, oldStatus, newStatus }
}

{
    type: "issue.assigned",
    payload: { issue, agentId }
}
```

#### Phase 2: Agent Task Creation (1-2 hours)

**Structured Output Parsing:**

```go
// In agents/processor.go
type StructuredBlock struct {
    Type    string // "issue", "artifact", "dialog", "message"
    Content map[string]interface{}
}

func parseStructuredOutput(response string) []StructuredBlock {
    // Parse blocks like:
    // @issue
    // title: Implement user authentication
    // description: Add JWT-based auth with login/signup
    // priority: high
    // ---

    blockRegex := regexp.MustCompile(`@(\w+)\n([\s\S]*?)---`)
    // Parse YAML-like content
}
```

**Agent System Prompts Enhancement:**

```go
systemPrompts := map[string]string{
    "product_manager": `You are a Product Manager AI agent.

When you identify tasks, create them using this format:

@issue
title: Short task title
description: Detailed description of what needs to be done
priority: high|medium|low
assignee: product_manager|backend_architect|frontend_developer
tags: feature, api, database
---

Always break down large requests into specific, actionable tasks.`,
}
```

**Example Agent Response:**
```
I'll help you build the authentication system. Let me break this down:

@issue
title: Design authentication database schema
description: Create users, sessions, and tokens tables with proper relationships
priority: high
assignee: backend_architect
tags: auth, database, schema
---

@issue
title: Implement JWT authentication endpoints
description: Create /login, /register, /refresh, /logout endpoints
priority: high
assignee: backend_architect
tags: auth, api, jwt
---

@issue
title: Build login/signup UI components
description: Create responsive login and signup forms with validation
priority: medium
assignee: frontend_developer
tags: auth, ui, forms
---

I've created 3 tasks. The backend architect should start with the schema.
```

#### Phase 3: Automatic Task Pickup (2-3 hours)

**Task Queue System:**

```go
// agents/queue.go
type TaskQueue struct {
    db        *sql.DB
    broadcast chan<- []byte
    agents    map[string]*AgentWorker
}

type AgentWorker struct {
    ID          string
    Type        string
    Status      string // idle, working, waiting
    CurrentTask *Issue
    Client      *openai.Client
}

func (q *TaskQueue) Start() {
    // Start background worker for each agent
    for agentType := range q.agents {
        go q.agentWorker(agentType)
    }
}

func (q *TaskQueue) agentWorker(agentType string) {
    for {
        // Wait until agent is idle
        if q.agents[agentType].Status != "idle" {
            time.Sleep(5 * time.Second)
            continue
        }

        // Get next task from queue
        task := q.getNextTask(agentType)
        if task == nil {
            time.Sleep(10 * time.Second)
            continue
        }

        // Execute task
        q.executeTask(agentType, task)
    }
}

func (q *TaskQueue) getNextTask(agentType string) *Issue {
    var issue Issue

    // Priority: urgent > high > medium > low
    // Then by queued_at (FIFO)
    err := q.db.QueryRow(`
        SELECT id, title, description, priority, status
        FROM issues
        WHERE assigned_agent_id = ? OR queued_to_agent_id = ?
        AND status = 'todo'
        ORDER BY
            CASE priority
                WHEN 'urgent' THEN 0
                WHEN 'high' THEN 1
                WHEN 'medium' THEN 2
                WHEN 'low' THEN 3
            END,
            queued_at ASC
        LIMIT 1
    `, agentType, agentType).Scan(&issue.ID, &issue.Title, &issue.Description, &issue.Priority, &issue.Status)

    if err == sql.ErrNoRows {
        return nil
    }

    return &issue
}

func (q *TaskQueue) executeTask(agentType string, task *Issue) {
    // Update task status
    q.db.Exec(`UPDATE issues SET status = 'inProgress', started_at = ? WHERE id = ?`, time.Now(), task.ID)

    // Update agent status
    q.agents[agentType].Status = "working"
    q.agents[agentType].CurrentTask = task

    // Broadcast status update
    q.broadcastAgentStatus(agentType, "working", task.ID)

    // Build context for task
    context := q.buildTaskContext(task)

    // Call OpenAI API
    response := q.callOpenAI(agentType, task, context)

    // Parse response for artifacts, sub-tasks, etc.
    blocks := parseStructuredOutput(response)

    // Execute blocks
    for _, block := range blocks {
        q.executeBlock(block, task, agentType)
    }

    // Mark task as complete (or review)
    q.db.Exec(`UPDATE issues SET status = 'review', completed_at = ? WHERE id = ?`, time.Now(), task.ID)

    // Update agent status
    q.agents[agentType].Status = "idle"
    q.agents[agentType].CurrentTask = nil

    // Broadcast completion
    q.broadcastTaskCompletion(task.ID, agentType)
}
```

**Task Context Building:**

```go
func (q *TaskQueue) buildTaskContext(task *Issue) string {
    var context strings.Builder

    // Project info
    context.WriteString("Project: ReplyChat - Multi-user AI collaboration platform\n\n")

    // Task details
    context.WriteString(fmt.Sprintf("Task: %s\n", task.Title))
    context.WriteString(fmt.Sprintf("Description: %s\n\n", task.Description))

    // Related artifacts
    rows, _ := q.db.Query(`
        SELECT title, content, type FROM artifacts
        WHERE issue_id = ?
        ORDER BY created_at DESC
    `, task.ID)

    context.WriteString("Related Artifacts:\n")
    for rows.Next() {
        var title, content, artType string
        rows.Scan(&title, &content, &artType)
        context.WriteString(fmt.Sprintf("- %s (%s)\n", title, artType))
    }

    // Recent messages from team
    rows, _ = q.db.Query(`
        SELECT content, sender_type FROM messages
        WHERE project_id = ?
        ORDER BY timestamp DESC
        LIMIT 10
    `, task.ProjectID)

    context.WriteString("\nRecent Team Discussion:\n")
    for rows.Next() {
        var content, senderType string
        rows.Scan(&content, &senderType)
        context.WriteString(fmt.Sprintf("- [%s]: %s\n", senderType, content))
    }

    return context.String()
}
```

**Approval Workflow:**

```go
// When task is in 'review' status
func approveIssueHandler(w http.ResponseWriter, r *http.Request) {
    issueID := r.URL.Query().Get("id")

    // Update status
    db.Exec(`UPDATE issues SET status = 'done', approved_at = ? WHERE id = ?`, time.Now(), issueID)

    // Notify agent (can pick up next task)
    broadcastIssueApproved(issueID)
}

func requestChangesHandler(w http.ResponseWriter, r *http.Request) {
    issueID := r.URL.Query().Get("id")
    feedback := r.FormValue("feedback")

    // Update status back to todo with feedback
    db.Exec(`UPDATE issues SET status = 'todo' WHERE id = ?`, issueID)

    // Add feedback as message
    saveMessage("system", fmt.Sprintf("Changes requested for task: %s", feedback))
}
```

---

## Message Persistence

### Current Problem
- ✅ Messages saved to database
- ❌ Messages not loaded on page load
- ❌ User only sees messages sent during current session
- ❌ No message history shown to new users

### Solution

#### Backend: Load Recent Messages (30 minutes)

```go
// In projectHandler
func projectHandler(w http.ResponseWriter, r *http.Request) {
    // ... existing session check ...

    // Load recent messages
    rows, err := db.Query(`
        SELECT m.id, m.sender_id, m.sender_type, m.content, m.timestamp,
               COALESCE(u.name, 'Agent') as sender_name
        FROM messages m
        LEFT JOIN users u ON m.sender_id = u.id AND m.sender_type = 'user'
        WHERE m.project_id = 'default'
        ORDER BY m.timestamp ASC
        LIMIT 100
    `)

    var messages []Message
    for rows.Next() {
        var msg Message
        rows.Scan(&msg.ID, &msg.SenderID, &msg.SenderType, &msg.Content, &msg.Timestamp, &msg.SenderName)
        messages = append(messages, msg)
    }

    data := map[string]interface{}{
        "Username": username,
        "Email":    email,
        "UserID":   userID,
        "Messages": messages, // Add to template data
    }

    renderTemplate(w, "project.html", data)
}
```

#### Frontend: Render Initial Messages (30 minutes)

```html
<!-- In project.html -->
<div id="messages" class="messages-area">
    <div class="system-message">
        <p>Welcome to ReplyChat! Start a conversation with your AI agents.</p>
    </div>

    {{range .Messages}}
    <div class="message {{.SenderType}}">
        <div class="message-avatar">
            {{if eq .SenderType "user"}}U{{else}}{{index (split .SenderName " ") 0 | slice 0 2}}{{end}}
        </div>
        <div class="message-content">
            <div class="message-sender">{{.SenderName}}</div>
            <div class="message-text">{{.Content}}</div>
        </div>
    </div>
    {{end}}
</div>
```

```javascript
// In app.js - auto-scroll to bottom on load
document.addEventListener('DOMContentLoaded', () => {
    messagesArea.scrollTop = messagesArea.scrollHeight;
});
```

#### Pagination & Infinite Scroll (optional, 1-2 hours)

```javascript
// Load older messages when scrolling to top
messagesArea.addEventListener('scroll', () => {
    if (messagesArea.scrollTop === 0 && !isLoadingMessages) {
        loadOlderMessages();
    }
});

async function loadOlderMessages() {
    isLoadingMessages = true;
    const oldestMessageId = document.querySelector('.message')?.dataset.messageId;

    const response = await fetch(`/api/messages?before=${oldestMessageId}&limit=50`);
    const messages = await response.json();

    // Prepend messages to top
    messages.reverse().forEach(msg => {
        prependMessage(msg);
    });

    isLoadingMessages = false;
}
```

---

## Agent File Editing System

### Vision
Agents can create, edit, and manage code files, similar to Claude Code or Cursor.

### Architecture Options

#### Option 1: Virtual File System (Sandbox)

**Pros:**
- Safe (no access to real filesystem)
- Easy to implement
- Can show diffs before applying

**Cons:**
- Files only exist in database
- No real execution
- Limited to demo/prototype

**Implementation:**
```go
// Database schema
CREATE TABLE files (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    path TEXT NOT NULL,
    content TEXT NOT NULL,
    language TEXT,
    created_by TEXT,
    created_at TIMESTAMP,
    updated_at TIMESTAMP,
    UNIQUE(project_id, path)
);

CREATE TABLE file_versions (
    id TEXT PRIMARY KEY,
    file_id TEXT NOT NULL,
    content TEXT NOT NULL,
    author TEXT,
    message TEXT,
    created_at TIMESTAMP,
    FOREIGN KEY (file_id) REFERENCES files(id)
);
```

**Agent Usage:**
```
@artifact
type: code
title: User Authentication Service
language: go
path: src/auth/service.go
---
package auth

import (
    "database/sql"
    "github.com/golang-jwt/jwt"
)

type AuthService struct {
    db *sql.DB
}

func (s *AuthService) Login(email, password string) (string, error) {
    // Login implementation
}
---
```

#### Option 2: Git-Based Workflow (Recommended for Production)

**Pros:**
- Real version control
- Can actually run code
- Proper development workflow
- Team can review via GitHub/GitLab
- Built-in collaboration

**Cons:**
- Needs git credentials management
- Security concerns (agents pushing code)
- More complex setup

**Implementation:**

```go
// Database schema for project git repos
CREATE TABLE project_repos (
    project_id TEXT PRIMARY KEY,
    git_url TEXT NOT NULL,
    branch TEXT DEFAULT 'main',
    access_token TEXT,
    last_sync TIMESTAMP,
    FOREIGN KEY (project_id) REFERENCES projects(id)
);

// Project workspace structure
workspaces/
├── project_123/
│   ├── .git/
│   ├── src/
│   │   ├── main.go
│   │   └── auth/
│   │       └── service.go
│   ├── go.mod
│   └── .replychat/
│       └── metadata.json

// Git operations for agents
type GitWorkspace struct {
    ProjectID string
    RepoURL   string
    Branch    string
    LocalPath string
}

func (g *GitWorkspace) Clone() error {
    cmd := exec.Command("git", "clone", g.RepoURL, g.LocalPath)
    return cmd.Run()
}

func (g *GitWorkspace) FetchAndRefresh() error {
    // Fetch latest changes
    cmd := exec.Command("git", "-C", g.LocalPath, "fetch", "origin", g.Branch)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("git fetch failed: %w", err)
    }

    // Check for conflicts
    statusCmd := exec.Command("git", "-C", g.LocalPath, "status", "--porcelain")
    output, _ := statusCmd.Output()

    if len(output) > 0 {
        // Stash local changes
        exec.Command("git", "-C", g.LocalPath, "stash").Run()
    }

    // Pull latest
    cmd = exec.Command("git", "-C", g.LocalPath, "pull", "origin", g.Branch)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("git pull failed: %w", err)
    }

    // Pop stash if we stashed
    if len(output) > 0 {
        exec.Command("git", "-C", g.LocalPath, "stash", "pop").Run()
    }

    return nil
}

func (g *GitWorkspace) CommitAndPush(message, agentName string) error {
    // Add all changes
    cmd := exec.Command("git", "-C", g.LocalPath, "add", ".")
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("git add failed: %w", err)
    }

    // Set git user for this commit
    exec.Command("git", "-C", g.LocalPath, "config", "user.name", agentName).Run()
    exec.Command("git", "-C", g.LocalPath, "config", "user.email", "agent@replychat.ai").Run()

    // Commit with co-authored tag
    commitMsg := fmt.Sprintf("%s\n\nCo-authored-by: %s <agent@replychat.ai>", message, agentName)
    cmd = exec.Command("git", "-C", g.LocalPath, "commit", "-m", commitMsg)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("git commit failed: %w", err)
    }

    // Push to origin
    cmd = exec.Command("git", "-C", g.LocalPath, "push", "origin", g.Branch)
    if err := cmd.Run(); err != nil {
        return fmt.Errorf("git push failed: %w", err)
    }

    return nil
}

// Agent task execution with git workflow
func (q *TaskQueue) executeTask(agentType string, task *Issue) {
    agent := q.agents[agentType]

    // Get project git workspace
    workspace := q.getWorkspace(task.ProjectID)

    // 1. FETCH AND REFRESH before starting
    log.Printf("agent: %s fetching latest code", agentType)
    if err := workspace.FetchAndRefresh(); err != nil {
        q.reportError(task, fmt.Sprintf("Failed to sync with repo: %v", err))
        return
    }

    // 2. Update task status
    q.db.Exec(`UPDATE issues SET status = 'inProgress', started_at = ? WHERE id = ?`, time.Now(), task.ID)
    q.broadcastAgentStatus(agentType, "working", task.ID)

    // 3. Build context with current repo state
    context := q.buildTaskContext(task, workspace)

    // 4. Call OpenAI API with enhanced prompt
    systemPrompt := fmt.Sprintf(`You are a %s AI agent working on a real codebase.

Git Repository: %s
Current Branch: %s

Before you received this task, I ran:
- git fetch origin %s
- git pull origin %s

The repository is up to date. You can now make changes.

When you're done, I will:
- Commit your changes with an appropriate message
- Push to the repository

Please create or modify files as needed to complete the task.`,
        agentType, workspace.RepoURL, workspace.Branch, workspace.Branch, workspace.Branch)

    response := q.callOpenAI(agentType, task, context, systemPrompt)

    // 5. Parse response and apply file changes
    blocks := parseStructuredOutput(response)
    var filesChanged []string

    for _, block := range blocks {
        if block.Type == "artifact" && block.Content["type"] == "code" {
            path := block.Content["path"].(string)
            content := block.Content["content"].(string)

            // Write file
            fullPath := filepath.Join(workspace.LocalPath, path)
            os.MkdirAll(filepath.Dir(fullPath), 0755)
            os.WriteFile(fullPath, []byte(content), 0644)

            filesChanged = append(filesChanged, path)
        }
    }

    // 6. COMMIT AND PUSH changes
    if len(filesChanged) > 0 {
        commitMessage := fmt.Sprintf("%s: %s\n\nFiles changed:\n%s",
            agentType, task.Title, strings.Join(filesChanged, "\n- "))

        log.Printf("agent: %s committing changes", agentType)
        if err := workspace.CommitAndPush(commitMessage, getAgentName(agentType)); err != nil {
            q.reportError(task, fmt.Sprintf("Failed to commit changes: %v", err))
            return
        }

        // Post commit info to chat
        q.postCommitNotification(task.ProjectID, agentType, filesChanged)
    }

    // 7. Mark task complete
    q.db.Exec(`UPDATE issues SET status = 'review', completed_at = ? WHERE id = ?`, time.Now(), task.ID)
    q.agents[agentType].Status = "idle"
    q.broadcastTaskCompletion(task.ID, agentType)
}

func (q *TaskQueue) postCommitNotification(projectID, agentType string, files []string) {
    message := fmt.Sprintf("✅ %s committed changes:\n%s",
        getAgentName(agentType),
        strings.Join(files, "\n- "))

    // Post to chat
    messageID := uuid.New().String()
    q.db.Exec(`
        INSERT INTO messages (id, project_id, sender_id, sender_type, content, message_type, timestamp)
        VALUES (?, ?, ?, ?, ?, ?, ?)
    `, messageID, projectID, agentType, "agent", message, "system", time.Now())

    // Broadcast
    response := map[string]interface{}{
        "type": "message.received",
        "payload": map[string]interface{}{
            "message": map[string]interface{}{
                "id":          messageID,
                "content":     message,
                "senderType":  "agent",
                "senderName":  getAgentName(agentType),
                "messageType": "system",
            },
        },
    }

    responseJSON, _ := json.Marshal(response)
    q.broadcast <- responseJSON
}
```

**Setup Flow:**

1. **Project Creation:**
```go
func createProjectHandler(w http.ResponseWriter, r *http.Request) {
    projectName := r.FormValue("name")
    gitURL := r.FormValue("git_url")
    accessToken := r.FormValue("access_token") // GitHub PAT

    projectID := uuid.New().String()

    // Create project
    db.Exec(`INSERT INTO projects (id, name, created_at) VALUES (?, ?, ?)`,
        projectID, projectName, time.Now())

    // Store repo info
    db.Exec(`INSERT INTO project_repos (project_id, git_url, access_token) VALUES (?, ?, ?)`,
        projectID, gitURL, accessToken)

    // Clone repo in background
    go cloneProjectRepo(projectID, gitURL, accessToken)

    http.Redirect(w, r, fmt.Sprintf("/project?id=%s", projectID), http.StatusSeeOther)
}
```

2. **Agent Git Configuration:**
```bash
# In agent's system prompt
git config --global user.name "Backend Architect Agent"
git config --global user.email "backend-architect@replychat.ai"

# For GitHub authentication
git config --global credential.helper store
echo "https://${ACCESS_TOKEN}:x-oauth-basic@github.com" > ~/.git-credentials
```

3. **UI for Git Setup:**
```html
<form action="/project/create" method="POST">
    <input name="name" placeholder="Project Name" required>
    <input name="git_url" placeholder="https://github.com/user/repo.git" required>
    <input name="access_token" type="password" placeholder="GitHub Personal Access Token" required>
    <button type="submit">Create Project</button>
</form>
```

**Agent System Prompt Addition:**
```go
systemPrompt := `You are a Backend Architect AI agent.

You can create and edit files using these formats:

@artifact
type: code
title: File description
language: go
path: src/service/auth.go
action: create|update
---
[file content here]
---

To run commands:
@command
cmd: go test ./...
---

Always explain what you're doing before creating files.`
```

#### Option 3: Hybrid Approach (Recommended)

1. **Development Phase:** Virtual filesystem (safe, fast iteration)
2. **Export Feature:** Export to real workspace when ready
3. **Execution:** Run in isolated Docker container

```go
type FileOperation struct {
    Type    string // create, update, delete
    Path    string
    Content string
    Message string
}

// Agent creates operations
ops := []FileOperation{
    {Type: "create", Path: "src/auth/service.go", Content: "..."},
    {Type: "create", Path: "src/auth/handler.go", Content: "..."},
}

// User reviews in UI (shows diffs)
// User approves

// Apply operations
func applyOperations(projectID string, ops []FileOperation) error {
    for _, op := range ops {
        switch op.Type {
        case "create":
            saveToVirtualFS(projectID, op.Path, op.Content)
        case "update":
            updateInVirtualFS(projectID, op.Path, op.Content)
        }
    }
}

// Export to real workspace
func exportProject(projectID string) error {
    files := loadAllFiles(projectID)
    workspacePath := fmt.Sprintf("workspaces/%s", projectID)

    for _, file := range files {
        writeToDisk(workspacePath, file.Path, file.Content)
    }

    return nil
}
```

### UI for File Management

**File Browser Component:**
```javascript
<div class="file-browser">
    <div class="file-tree">
        <div class="folder" data-path="src">
            <span class="folder-icon">📁</span> src
            <div class="file" data-path="src/main.go">
                <span class="file-icon">📄</span> main.go
            </div>
        </div>
    </div>

    <div class="file-viewer">
        <div class="file-header">
            <span class="file-path">src/main.go</span>
            <button class="btn-edit">Edit</button>
        </div>
        <pre class="code-content"><code class="language-go">
            <!-- File content with syntax highlighting -->
        </code></pre>
    </div>
</div>
```

**Diff Viewer for Changes:**
```javascript
// Show before/after when agent proposes changes
<div class="diff-viewer">
    <div class="diff-header">
        Changes to src/auth/service.go
    </div>
    <div class="diff-content">
        <div class="diff-line removed">-  old code</div>
        <div class="diff-line added">+  new code</div>
    </div>
    <div class="diff-actions">
        <button onclick="approveChange()">Approve</button>
        <button onclick="rejectChange()">Reject</button>
    </div>
</div>
```

---

## Project Isolation

### Current Problem
- All users share "default" project
- No way to create separate workspaces
- No project membership management

### Solution: URL-Based Projects (Quick, 30 minutes)

**Backend:**
```go
// projectHandler - read project from URL
func projectHandler(w http.ResponseWriter, r *http.Request) {
    projectID := r.URL.Query().Get("id")
    if projectID == "" {
        projectID = "default"
    }

    // Validate project exists or create
    var exists bool
    db.QueryRow(`SELECT EXISTS(SELECT 1 FROM projects WHERE id = ?)`, projectID).Scan(&exists)

    if !exists {
        db.Exec(`INSERT INTO projects (id, name, created_at) VALUES (?, ?, ?)`,
            projectID, projectID, time.Now())
    }

    // Load project-specific messages
    messages := loadMessages(projectID)

    data := map[string]interface{}{
        "ProjectID": projectID,
        "Messages":  messages,
    }

    renderTemplate(w, "project.html", data)
}
```

**Frontend:**
```javascript
// Read project from URL
const urlParams = new URLSearchParams(window.location.search);
const projectId = urlParams.get('id') || 'default';

// Use in WebSocket connection
const wsUrl = `${protocol}//${host}/ws?projectId=${projectId}`;
```

**Usage:**
```
http://localhost:8080/project?id=team-alpha
http://localhost:8080/project?id=engineering-2024
http://localhost:8080/project?id=hackathon-project
```

### Future: Full Project Management UI (2-3 hours)

- Dashboard with project list
- Create new project button
- Invite members via email
- Project settings page
- Access control (owner, member, viewer)

---

## Agent Context & Memory

### Current Limitation
Agents only see current message, no conversation history.

### Solution: Context Window Management

```go
func buildAgentContext(projectID, currentMessage string) []openai.ChatCompletionMessage {
    var messages []openai.ChatCompletionMessage

    // System prompt
    messages = append(messages, openai.ChatCompletionMessage{
        Role:    openai.ChatMessageRoleSystem,
        Content: getSystemPrompt(agentType),
    })

    // Recent conversation (last 10 messages)
    rows, _ := db.Query(`
        SELECT sender_type, content FROM messages
        WHERE project_id = ?
        ORDER BY timestamp DESC
        LIMIT 10
    `, projectID)

    var recentMsgs []Message
    for rows.Next() {
        var msg Message
        rows.Scan(&msg.SenderType, &msg.Content)
        recentMsgs = append([]Message{msg}, recentMsgs...) // Prepend to get chronological order
    }

    for _, msg := range recentMsgs {
        role := openai.ChatMessageRoleUser
        if msg.SenderType == "agent" {
            role = openai.ChatMessageRoleAssistant
        }

        messages = append(messages, openai.ChatCompletionMessage{
            Role:    role,
            Content: msg.Content,
        })
    }

    // Current message
    messages = append(messages, openai.ChatCompletionMessage{
        Role:    openai.ChatMessageRoleUser,
        Content: currentMessage,
    })

    return messages
}
```

**Token Management:**
```go
func estimateTokens(text string) int {
    // Rough estimate: 1 token ≈ 4 characters
    return len(text) / 4
}

func pruneContext(messages []openai.ChatCompletionMessage, maxTokens int) []openai.ChatCompletionMessage {
    totalTokens := 0
    for _, msg := range messages {
        totalTokens += estimateTokens(msg.Content)
    }

    if totalTokens <= maxTokens {
        return messages
    }

    // Keep system prompt + recent messages that fit
    systemPrompt := messages[0]
    recentMessages := messages[1:]

    // Reverse to keep most recent
    var pruned []openai.ChatCompletionMessage
    pruned = append(pruned, systemPrompt)

    for i := len(recentMessages) - 1; i >= 0; i-- {
        msgTokens := estimateTokens(recentMessages[i].Content)
        if totalTokens-msgTokens < maxTokens {
            pruned = append([]openai.ChatCompletionMessage{recentMessages[i]}, pruned...)
            totalTokens -= msgTokens
        }
    }

    return pruned
}
```

---

## Implementation Priority

### Phase 1: Core Improvements (1-2 days)
1. ✅ Fix message persistence (load on page load)
2. ✅ Add conversation context to agents
3. ✅ URL-based project isolation

### Phase 2: Task Management (2-3 days)
1. Kanban board UI
2. Structured output parsing (@issue blocks)
3. Task approval workflow

### Phase 3: Autonomous Agents (3-5 days)
1. Task queue system
2. Automatic task pickup
3. Agent status tracking
4. Task context building

### Phase 4: File System (3-5 days)
1. Virtual filesystem with database
2. File viewer UI with syntax highlighting
3. Diff viewer for changes
4. Agent file operations (@artifact blocks)

### Phase 5: Advanced Features (1 week+)
1. Real workspace export
2. Docker-based execution
3. Full project management
4. Agent collaboration features
5. Webhook integrations

---

## Technical Considerations

### Performance
- Message pagination (100 initial, load more on scroll)
- Task queue polling vs. event-driven
- WebSocket connection limits (1000+ concurrent)
- Database indexing for queries

### Security
- Validate agent file operations (no path traversal)
- Sandbox command execution
- Rate limit API calls
- Validate structured output

### Cost Management
- Cache agent responses
- Use cheaper models for simple tasks (GPT-4o-mini)
- Implement token limits per task
- Monitor API usage per project

### UX Improvements
- Loading states for agent responses
- Progress indicators for long tasks
- Notifications for task completion
- Undo/redo for approvals
- Keyboard shortcuts

---

## Agent Git Workflow Summary

### Before Starting Task:
1. **Fetch:** `git fetch origin main`
2. **Pull:** `git pull origin main`
3. **Read:** Read relevant files from workspace
4. **Execute:** Call OpenAI API with file context

### After Completing Task:
1. **Write:** Write changed files to workspace
2. **Add:** `git add .`
3. **Commit:** `git commit -m "Agent: Task title\n\nFiles changed: ..."` with co-author tag
4. **Push:** `git push origin main`
5. **Notify:** Post commit notification to chat

### Benefits:
- ✅ Real version control history
- ✅ Team can see changes on GitHub
- ✅ Easy to review and revert
- ✅ Agents always work with latest code
- ✅ No merge conflicts (sequential execution)
- ✅ Co-authored commits show agent contributions

---

## Next Steps

### Immediate (Today)
1. Fix message persistence (30 min)
2. Update README with current features

### Short-term (This Week)
1. Build basic Kanban UI (2-3 hours)
2. Implement structured output parsing (1-2 hours)
3. Add URL-based project isolation (30 min)

### Medium-term (Next 2 Weeks)
1. Implement task queue system (2-3 days)
2. Add git workspace integration (2-3 days)
3. Build file viewer UI (1-2 days)

### Long-term (Next Month)
1. Automatic task pickup and execution (3-5 days)
2. Agent context and memory (2-3 days)
3. Full project management UI (3-5 days)

Start with message persistence since it's quick and improves UX immediately.
