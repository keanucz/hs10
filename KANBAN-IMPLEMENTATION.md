# Kanban Board Implementation Plan

Quick start guide for building the Kanban board feature.

## Overview

Add a visual Kanban board to manage tasks that agents propose and execute.

## Database Schema (Already Exists!)

```sql
CREATE TABLE issues (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL,
    title TEXT NOT NULL,
    description TEXT,
    priority TEXT NOT NULL,        -- urgent, high, medium, low
    status TEXT NOT NULL,           -- proposed, todo, inProgress, review, done
    created_by TEXT NOT NULL,
    created_by_type TEXT NOT NULL,  -- user, agent
    assigned_agent_id TEXT,
    queued_at TIMESTAMP,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    tags TEXT,
    FOREIGN KEY (project_id) REFERENCES projects(id),
    FOREIGN KEY (assigned_agent_id) REFERENCES agents(id)
);
```

## Implementation Steps

### Step 1: Create Kanban Page Template (30 min)

**File:** `src/template/kanban.html`

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Kanban - ReplyChat</title>
    <link rel="stylesheet" href="/static/styles.css">
    <link rel="stylesheet" href="/static/kanban.css">
</head>
<body>
    <div class="app-container">
        <aside class="sidebar">
            <div class="sidebar-header">
                <h2>ReplyChat</h2>
                <div class="user-info">
                    <span class="user-name">{{.Username}}</span>
                    <a href="/logout" class="logout-link">Logout</a>
                </div>
            </div>

            <nav class="sidebar-nav">
                <a href="/project" class="nav-link">💬 Chat</a>
                <a href="/kanban" class="nav-link active">📋 Kanban</a>
            </nav>

            <div class="kanban-filters">
                <h3>Filters</h3>
                <div class="filter-group">
                    <label>Agent</label>
                    <select id="filter-agent">
                        <option value="">All Agents</option>
                        <option value="product_manager">Product Manager</option>
                        <option value="backend_architect">Backend Architect</option>
                        <option value="frontend_developer">Frontend Developer</option>
                    </select>
                </div>
                <div class="filter-group">
                    <label>Priority</label>
                    <select id="filter-priority">
                        <option value="">All Priorities</option>
                        <option value="urgent">Urgent</option>
                        <option value="high">High</option>
                        <option value="medium">Medium</option>
                        <option value="low">Low</option>
                    </select>
                </div>
            </div>
        </aside>

        <main class="kanban-main">
            <div class="kanban-header">
                <h1>Task Board</h1>
                <div class="header-actions">
                    <button id="create-task-btn" class="btn-primary">+ New Task</button>
                    <button id="refresh-btn" class="btn-secondary">↻ Refresh</button>
                </div>
            </div>

            <div class="kanban-board" id="kanban-board">
                <div class="kanban-column" data-status="proposed">
                    <div class="column-header">
                        <span class="column-title">Proposed</span>
                        <span class="column-count" id="count-proposed">0</span>
                    </div>
                    <div class="column-content" id="column-proposed">
                        <!-- Task cards go here -->
                    </div>
                </div>

                <div class="kanban-column" data-status="todo">
                    <div class="column-header">
                        <span class="column-title">To Do</span>
                        <span class="column-count" id="count-todo">0</span>
                    </div>
                    <div class="column-content" id="column-todo">
                        <!-- Task cards go here -->
                    </div>
                </div>

                <div class="kanban-column" data-status="inProgress">
                    <div class="column-header">
                        <span class="column-title">In Progress</span>
                        <span class="column-count" id="count-inProgress">0</span>
                    </div>
                    <div class="column-content" id="column-inProgress">
                        <!-- Task cards go here -->
                    </div>
                </div>

                <div class="kanban-column" data-status="review">
                    <div class="column-header">
                        <span class="column-title">Review</span>
                        <span class="column-count" id="count-review">0</span>
                    </div>
                    <div class="column-content" id="column-review">
                        <!-- Task cards go here -->
                    </div>
                </div>

                <div class="kanban-column" data-status="done">
                    <div class="column-header">
                        <span class="column-title">Done</span>
                        <span class="column-count" id="count-done">0</span>
                    </div>
                    <div class="column-content" id="column-done">
                        <!-- Task cards go here -->
                    </div>
                </div>
            </div>
        </main>
    </div>

    <!-- Task Detail Modal -->
    <div id="task-modal" class="modal" style="display: none;">
        <div class="modal-content">
            <div class="modal-header">
                <h2 id="modal-title">Task Details</h2>
                <button class="modal-close" onclick="closeTaskModal()">&times;</button>
            </div>
            <div class="modal-body" id="modal-body">
                <!-- Task details populated by JS -->
            </div>
        </div>
    </div>

    <!-- Create Task Modal -->
    <div id="create-modal" class="modal" style="display: none;">
        <div class="modal-content">
            <div class="modal-header">
                <h2>Create New Task</h2>
                <button class="modal-close" onclick="closeCreateModal()">&times;</button>
            </div>
            <div class="modal-body">
                <form id="create-task-form">
                    <div class="form-group">
                        <label for="task-title">Title</label>
                        <input type="text" id="task-title" required>
                    </div>
                    <div class="form-group">
                        <label for="task-description">Description</label>
                        <textarea id="task-description" rows="4"></textarea>
                    </div>
                    <div class="form-row">
                        <div class="form-group">
                            <label for="task-priority">Priority</label>
                            <select id="task-priority">
                                <option value="low">Low</option>
                                <option value="medium" selected>Medium</option>
                                <option value="high">High</option>
                                <option value="urgent">Urgent</option>
                            </select>
                        </div>
                        <div class="form-group">
                            <label for="task-agent">Assign To</label>
                            <select id="task-agent">
                                <option value="">Unassigned</option>
                                <option value="product_manager">Product Manager</option>
                                <option value="backend_architect">Backend Architect</option>
                                <option value="frontend_developer">Frontend Developer</option>
                            </select>
                        </div>
                    </div>
                    <div class="form-actions">
                        <button type="button" onclick="closeCreateModal()" class="btn-secondary">Cancel</button>
                        <button type="submit" class="btn-primary">Create Task</button>
                    </div>
                </form>
            </div>
        </div>
    </div>

    <script>
        window.userData = {
            userId: "{{.UserID}}",
            username: "{{.Username}}"
        };
    </script>
    <script src="/static/kanban.js"></script>
</body>
</html>
```

### Step 2: Create Kanban CSS (30 min)

**File:** `src/static/kanban.css`

```css
.sidebar-nav {
    padding: 1rem 1.5rem;
    border-bottom: 1px solid var(--border);
}

.nav-link {
    display: block;
    padding: 0.75rem 1rem;
    margin-bottom: 0.5rem;
    border-radius: 6px;
    text-decoration: none;
    color: var(--text-primary);
    transition: background-color 0.2s;
}

.nav-link:hover {
    background-color: var(--background);
}

.nav-link.active {
    background-color: var(--primary-color);
    color: white;
}

.kanban-filters {
    padding: 1.5rem;
}

.kanban-filters h3 {
    font-size: 0.875rem;
    text-transform: uppercase;
    color: var(--text-secondary);
    margin-bottom: 1rem;
}

.filter-group {
    margin-bottom: 1rem;
}

.filter-group label {
    display: block;
    font-size: 0.875rem;
    font-weight: 500;
    margin-bottom: 0.5rem;
}

.filter-group select {
    width: 100%;
    padding: 0.5rem;
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 0.875rem;
}

.kanban-main {
    display: flex;
    flex-direction: column;
    background-color: var(--background);
    overflow: hidden;
    min-height: 0;
}

.kanban-header {
    padding: 1.5rem;
    background-color: var(--surface);
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.kanban-header h1 {
    font-size: 1.5rem;
}

.header-actions {
    display: flex;
    gap: 0.75rem;
}

.kanban-board {
    flex: 1;
    display: flex;
    gap: 1.5rem;
    padding: 1.5rem;
    overflow-x: auto;
    overflow-y: hidden;
}

.kanban-column {
    flex: 0 0 300px;
    display: flex;
    flex-direction: column;
    background-color: var(--surface);
    border-radius: 8px;
    border: 1px solid var(--border);
}

.column-header {
    padding: 1rem;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.column-title {
    font-weight: 600;
    font-size: 0.875rem;
    text-transform: uppercase;
    color: var(--text-secondary);
}

.column-count {
    background-color: var(--background);
    padding: 0.25rem 0.5rem;
    border-radius: 12px;
    font-size: 0.75rem;
    font-weight: 600;
}

.column-content {
    flex: 1;
    padding: 1rem;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    min-height: 0;
}

.task-card {
    background-color: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
    padding: 1rem;
    cursor: pointer;
    transition: all 0.2s;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.task-card:hover {
    box-shadow: 0 4px 6px rgba(0, 0, 0, 0.15);
    transform: translateY(-2px);
}

.task-card.dragging {
    opacity: 0.5;
    cursor: grabbing;
}

.task-priority {
    display: inline-block;
    padding: 0.25rem 0.5rem;
    border-radius: 4px;
    font-size: 0.75rem;
    font-weight: 600;
    margin-bottom: 0.5rem;
}

.task-priority.urgent {
    background-color: #fee;
    color: #c00;
}

.task-priority.high {
    background-color: #fef0e5;
    color: #f59e0b;
}

.task-priority.medium {
    background-color: #e5f3ff;
    color: #3b82f6;
}

.task-priority.low {
    background-color: #f0f0f0;
    color: #666;
}

.task-title {
    font-weight: 600;
    margin-bottom: 0.5rem;
    color: var(--text-primary);
}

.task-description {
    font-size: 0.875rem;
    color: var(--text-secondary);
    margin-bottom: 0.75rem;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
}

.task-meta {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 0.75rem;
    color: var(--text-secondary);
}

.task-agent {
    display: flex;
    align-items: center;
    gap: 0.25rem;
}

.task-agent-avatar {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    background-color: var(--primary-color);
    color: white;
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 0.625rem;
    font-weight: 600;
}

.modal {
    position: fixed;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background-color: rgba(0, 0, 0, 0.5);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 1000;
}

.modal-content {
    background-color: var(--surface);
    border-radius: 8px;
    width: 90%;
    max-width: 600px;
    max-height: 90vh;
    overflow: hidden;
    display: flex;
    flex-direction: column;
}

.modal-header {
    padding: 1.5rem;
    border-bottom: 1px solid var(--border);
    display: flex;
    justify-content: space-between;
    align-items: center;
}

.modal-header h2 {
    margin: 0;
    font-size: 1.25rem;
}

.modal-close {
    background: none;
    border: none;
    font-size: 1.5rem;
    cursor: pointer;
    color: var(--text-secondary);
    padding: 0;
    width: 32px;
    height: 32px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
}

.modal-close:hover {
    background-color: var(--background);
}

.modal-body {
    padding: 1.5rem;
    overflow-y: auto;
}

.form-row {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 1rem;
}

.form-actions {
    display: flex;
    justify-content: flex-end;
    gap: 0.75rem;
    margin-top: 1.5rem;
}

.task-actions {
    display: flex;
    gap: 0.5rem;
    margin-top: 1rem;
}

.btn-approve {
    background-color: var(--success);
    color: white;
    border: none;
    padding: 0.5rem 1rem;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
}

.btn-reject {
    background-color: var(--danger);
    color: white;
    border: none;
    padding: 0.5rem 1rem;
    border-radius: 4px;
    cursor: pointer;
    font-size: 0.875rem;
}
```

### Step 3: Create Kanban JavaScript (1-2 hours)

**File:** `src/static/kanban.js`

```javascript
let tasks = [];
let draggedTask = null;

// Load tasks on page load
document.addEventListener('DOMContentLoaded', () => {
    loadTasks();
    setupEventListeners();
});

function setupEventListeners() {
    document.getElementById('create-task-btn').addEventListener('click', openCreateModal);
    document.getElementById('refresh-btn').addEventListener('click', loadTasks);
    document.getElementById('create-task-form').addEventListener('submit', handleCreateTask);
    document.getElementById('filter-agent').addEventListener('change', filterTasks);
    document.getElementById('filter-priority').addEventListener('change', filterTasks);
}

async function loadTasks() {
    try {
        const response = await fetch('/api/issues?project_id=default');
        const data = await response.json();
        tasks = data.issues || [];
        renderTasks();
    } catch (err) {
        console.error('Failed to load tasks:', err);
    }
}

function renderTasks() {
    // Clear all columns
    const statuses = ['proposed', 'todo', 'inProgress', 'review', 'done'];
    statuses.forEach(status => {
        const column = document.getElementById(`column-${status}`);
        column.innerHTML = '';
    });

    // Get filter values
    const filterAgent = document.getElementById('filter-agent').value;
    const filterPriority = document.getElementById('filter-priority').value;

    // Filter tasks
    let filteredTasks = tasks;
    if (filterAgent) {
        filteredTasks = filteredTasks.filter(t => t.assigned_agent_id === filterAgent);
    }
    if (filterPriority) {
        filteredTasks = filteredTasks.filter(t => t.priority === filterPriority);
    }

    // Render tasks in columns
    filteredTasks.forEach(task => {
        const card = createTaskCard(task);
        const column = document.getElementById(`column-${task.status}`);
        if (column) {
            column.appendChild(card);
        }
    });

    // Update counts
    statuses.forEach(status => {
        const count = filteredTasks.filter(t => t.status === status).length;
        const countEl = document.getElementById(`count-${status}`);
        if (countEl) {
            countEl.textContent = count;
        }
    });
}

function createTaskCard(task) {
    const card = document.createElement('div');
    card.className = 'task-card';
    card.draggable = true;
    card.dataset.taskId = task.id;

    card.addEventListener('click', () => openTaskModal(task));
    card.addEventListener('dragstart', handleDragStart);
    card.addEventListener('dragend', handleDragEnd);

    const agentNames = {
        'product_manager': 'PM',
        'backend_architect': 'BA',
        'frontend_developer': 'FE'
    };

    card.innerHTML = `
        <span class="task-priority ${task.priority}">${task.priority.toUpperCase()}</span>
        <div class="task-title">${escapeHtml(task.title)}</div>
        <div class="task-description">${escapeHtml(task.description || '')}</div>
        <div class="task-meta">
            <div class="task-agent">
                ${task.assigned_agent_id ? `
                    <span class="task-agent-avatar">${agentNames[task.assigned_agent_id] || 'A'}</span>
                    <span>${formatAgentName(task.assigned_agent_id)}</span>
                ` : '<span>Unassigned</span>'}
            </div>
            <span>${formatDate(task.queued_at || task.created_at)}</span>
        </div>
    `;

    return card;
}

function handleDragStart(e) {
    draggedTask = e.currentTarget;
    e.currentTarget.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
}

function handleDragEnd(e) {
    e.currentTarget.classList.remove('dragging');
}

// Setup drop zones
document.querySelectorAll('.column-content').forEach(column => {
    column.addEventListener('dragover', handleDragOver);
    column.addEventListener('drop', handleDrop);
});

function handleDragOver(e) {
    e.preventDefault();
    e.dataTransfer.dropEffect = 'move';
}

async function handleDrop(e) {
    e.preventDefault();
    if (!draggedTask) return;

    const column = e.currentTarget;
    const newStatus = column.id.replace('column-', '');
    const taskId = draggedTask.dataset.taskId;

    try {
        const response = await fetch(`/api/issues/${taskId}/status`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status: newStatus })
        });

        if (response.ok) {
            await loadTasks();
        }
    } catch (err) {
        console.error('Failed to update task:', err);
    }
}

function openCreateModal() {
    document.getElementById('create-modal').style.display = 'flex';
}

function closeCreateModal() {
    document.getElementById('create-modal').style.display = 'none';
    document.getElementById('create-task-form').reset();
}

async function handleCreateTask(e) {
    e.preventDefault();

    const title = document.getElementById('task-title').value;
    const description = document.getElementById('task-description').value;
    const priority = document.getElementById('task-priority').value;
    const assignedAgentId = document.getElementById('task-agent').value;

    try {
        const response = await fetch('/api/issues', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                project_id: 'default',
                title,
                description,
                priority,
                status: 'todo',
                assigned_agent_id: assignedAgentId || null,
                created_by: window.userData.userId,
                created_by_type: 'user'
            })
        });

        if (response.ok) {
            closeCreateModal();
            await loadTasks();
        }
    } catch (err) {
        console.error('Failed to create task:', err);
    }
}

function openTaskModal(task) {
    const modal = document.getElementById('task-modal');
    const title = document.getElementById('modal-title');
    const body = document.getElementById('modal-body');

    title.textContent = task.title;

    const agentFullNames = {
        'product_manager': 'Product Manager',
        'backend_architect': 'Backend Architect',
        'frontend_developer': 'Frontend Developer'
    };

    body.innerHTML = `
        <div class="task-detail">
            <div class="form-group">
                <label>Priority</label>
                <span class="task-priority ${task.priority}">${task.priority.toUpperCase()}</span>
            </div>
            <div class="form-group">
                <label>Status</label>
                <p>${formatStatus(task.status)}</p>
            </div>
            <div class="form-group">
                <label>Description</label>
                <p>${escapeHtml(task.description || 'No description')}</p>
            </div>
            <div class="form-group">
                <label>Assigned To</label>
                <p>${task.assigned_agent_id ? agentFullNames[task.assigned_agent_id] : 'Unassigned'}</p>
            </div>
            <div class="form-group">
                <label>Created</label>
                <p>${formatDate(task.queued_at || task.created_at)}</p>
            </div>
            ${task.status === 'proposed' ? `
                <div class="task-actions">
                    <button class="btn-approve" onclick="approveTask('${task.id}')">✓ Approve</button>
                    <button class="btn-reject" onclick="rejectTask('${task.id}')">✗ Reject</button>
                </div>
            ` : ''}
        </div>
    `;

    modal.style.display = 'flex';
}

function closeTaskModal() {
    document.getElementById('task-modal').style.display = 'none';
}

async function approveTask(taskId) {
    try {
        const response = await fetch(`/api/issues/${taskId}/status`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ status: 'todo' })
        });

        if (response.ok) {
            closeTaskModal();
            await loadTasks();
        }
    } catch (err) {
        console.error('Failed to approve task:', err);
    }
}

async function rejectTask(taskId) {
    try {
        const response = await fetch(`/api/issues/${taskId}`, {
            method: 'DELETE'
        });

        if (response.ok) {
            closeTaskModal();
            await loadTasks();
        }
    } catch (err) {
        console.error('Failed to reject task:', err);
    }
}

function filterTasks() {
    renderTasks();
}

// Helper functions
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatAgentName(agentId) {
    const names = {
        'product_manager': 'Product Manager',
        'backend_architect': 'Backend Architect',
        'frontend_developer': 'Frontend Developer'
    };
    return names[agentId] || agentId;
}

function formatStatus(status) {
    const statuses = {
        'proposed': 'Proposed',
        'todo': 'To Do',
        'inProgress': 'In Progress',
        'review': 'Review',
        'done': 'Done'
    };
    return statuses[status] || status;
}

function formatDate(dateStr) {
    if (!dateStr) return 'N/A';
    const date = new Date(dateStr);
    const now = new Date();
    const diff = now - date;
    const days = Math.floor(diff / (1000 * 60 * 60 * 24));

    if (days === 0) return 'Today';
    if (days === 1) return 'Yesterday';
    if (days < 7) return `${days} days ago`;
    return date.toLocaleDateString();
}

// Close modals when clicking outside
document.addEventListener('click', (e) => {
    if (e.target.classList.contains('modal')) {
        e.target.style.display = 'none';
    }
});
```

### Step 4: Add Go Backend Routes (1 hour)

**Add to `src/main.go`:**

```go
// Add after existing routes in main()
mux.HandleFunc("/kanban", kanbanHandler)
mux.HandleFunc("/api/issues", issuesAPIHandler)
mux.HandleFunc("/api/issues/", issueAPIHandler) // For specific issue operations

// Handler functions
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

	var username, email string
	db.QueryRow(`SELECT name, email FROM users WHERE id = ?`, userID).Scan(&username, &email)

	data := map[string]interface{}{
		"Username": username,
		"Email":    email,
		"UserID":   userID,
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

	var issues []map[string]interface{}
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
	// Extract issue ID from URL
	path := strings.TrimPrefix(r.URL.Path, "/api/issues/")
	parts := strings.Split(path, "/")
	issueID := parts[0]

	if issueID == "" {
		http.Error(w, "Issue ID required", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPUT:
		if len(parts) > 1 && parts[1] == "status" {
			updateIssueStatusHandler(w, r, issueID)
		} else {
			http.Error(w, "Invalid endpoint", http.StatusBadRequest)
		}
	case http.MethodDELET:
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
```

## Testing Plan

1. **Create kanban.html template**
2. **Create kanban.css styling**
3. **Create kanban.js functionality**
4. **Add Go routes and handlers**
5. **Test:**
   - Navigate to `/kanban`
   - Create new task
   - Drag task between columns
   - View task details
   - Approve/reject proposed tasks
   - Filter by agent and priority

## Time Estimate

- Templates & CSS: 1 hour
- JavaScript: 1-2 hours
- Go backend: 1 hour
- Testing & fixes: 30 min

**Total: 3-4 hours**

## Quick Start

1. Copy kanban.html to `src/template/`
2. Copy kanban.css to `src/static/`
3. Copy kanban.js to `src/static/`
4. Add Go routes to `src/main.go`
5. Run `make dev`
6. Go to `/kanban`
