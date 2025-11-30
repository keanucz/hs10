let tasks = [];
let draggedTask = null;
const agentQueueSummary = document.getElementById('agent-queue-summary');

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
        const projectId = window.userData && window.userData.projectId ? window.userData.projectId : 'default';
        const response = await fetch(`/api/issues?project_id=${projectId}`);
        const data = await response.json();
        tasks = data.issues || [];
        renderTasks();
        await loadAgentQueues();
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

    if (task.queued_agent_id) {
        card.classList.add('task-queued');
    }

    card.addEventListener('click', () => openTaskModal(task));
    card.addEventListener('dragstart', handleDragStart);
    card.addEventListener('dragend', handleDragEnd);

    const agentNames = {
        'product_manager': 'PM',
        'backend_architect': 'BA',
        'frontend_developer': 'FE',
        'qa_tester': 'QA',
        'devops_engineer': 'DE'
    };

    card.innerHTML = `
        <span class="task-priority ${task.priority}">${task.priority.toUpperCase()}</span>
        <div class="task-title">${escapeHtml(task.title)}</div>
        <div class="task-description">${escapeHtml(task.description || '')}</div>
        ${task.queued_agent_id ? `<div class="task-queue-badge">Queued → ${formatAgentName(task.queued_agent_id)}</div>` : ''}
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
    const projectId = window.userData && window.userData.projectId ? window.userData.projectId : 'default';

    try {
        const response = await fetch('/api/issues', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                project_id: projectId,
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

async function loadAgentQueues() {
    if (!agentQueueSummary) return;

    try {
        const projectId = window.userData && window.userData.projectId ? window.userData.projectId : 'default';
        const response = await fetch(`/api/agent-queues?project_id=${projectId}`);
        if (!response.ok) return;

        const data = await response.json();
        renderAgentQueues(data.queues || []);
    } catch (err) {
        console.error('Failed to load agent queues:', err);
    }
}

function renderAgentQueues(stats) {
    if (!agentQueueSummary) return;

    const statMap = {};
    stats.forEach(stat => {
        if (stat && stat.agent_id) {
            statMap[stat.agent_id] = stat;
        }
    });

    agentQueueSummary.querySelectorAll('.agent-queue-chip').forEach(chip => {
        const agentId = chip.dataset.agentId;
        const countEl = chip.querySelector('.agent-queue-count');
        const stat = statMap[agentId];
        const queueDepth = stat ? stat.queue_depth : 0;

        if (countEl) {
            countEl.textContent = queueDepth;
        }

        if (stat && stat.status === 'working') {
            chip.classList.add('queued');
        } else if (queueDepth > 0) {
            chip.classList.add('queued');
        } else {
            chip.classList.remove('queued');
        }
    });
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
                <label>Queue Status</label>
                <p>${task.queued_agent_id ? `Queued → ${formatAgentName(task.queued_agent_id)}` : 'Not queued'}</p>
            </div>
            <div class="form-group">
                <label>Created</label>
                <p>${formatDate(task.created_at)}</p>
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
