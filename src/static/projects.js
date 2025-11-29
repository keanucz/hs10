let currentInviteProject = null;

document.addEventListener('DOMContentLoaded', () => {
    console.log('Projects page loaded');
    console.log('Create button exists:', !!document.getElementById('create-project-btn'));
    console.log('Form exists:', !!document.getElementById('create-project-form'));
    loadProjects();
    setupEventListeners();
});

function setupEventListeners() {
    document.getElementById('create-project-btn').addEventListener('click', openCreateModal);
    document.getElementById('create-project-form').addEventListener('submit', handleCreateProject);
    document.getElementById('repo-option').addEventListener('change', handleWorkspaceSourceChange);
    handleWorkspaceSourceChange();
}

async function loadProjects() {
    console.log('Loading projects...');
    try {
        const response = await fetch('/api/projects');
        console.log('Projects response status:', response.status);

        if (!response.ok) {
            const error = await response.text();
            console.error('Failed to load projects:', error);
            document.getElementById('projects-grid').innerHTML = '<div class="empty-state">Failed to load projects: ' + error + '</div>';
            return;
        }

        const data = await response.json();
        console.log('Projects data:', data);
        renderProjects(data.projects || []);
    } catch (err) {
        console.error('Failed to load projects:', err);
        document.getElementById('projects-grid').innerHTML = '<div class="empty-state">Failed to load projects: ' + err.message + '</div>';
    }
}

function renderProjects(projects) {
    const grid = document.getElementById('projects-grid');

    if (projects.length === 0) {
        grid.innerHTML = '<div class="empty-state">No projects yet. Create your first project to get started!</div>';
        return;
    }

    grid.innerHTML = '';
    projects.forEach(project => {
        const card = createProjectCard(project);
        grid.appendChild(card);
    });
}

function createProjectCard(project) {
    const card = document.createElement('div');
    card.className = 'project-card';

    const isOwner = project.owner_id === window.userData.userId;

    card.innerHTML = `
        <div class="project-card-header">
            <h3 class="project-name">${escapeHtml(project.name)}</h3>
            <span class="project-role ${isOwner ? 'owner' : ''}">${isOwner ? 'Owner' : 'Member'}</span>
        </div>
        <p class="project-description">${escapeHtml(project.description || 'No description')}</p>
        <div class="project-meta">
            <div class="project-members">
                👥 ${project.member_count || 1} member${(project.member_count || 1) !== 1 ? 's' : ''}
            </div>
            <div class="project-date">
                ${formatDate(project.created_at)}
            </div>
        </div>
        <div class="project-actions">
            <button class="btn-enter" onclick="enterProject('${project.id}')">Open Project</button>
            ${isOwner ? `<button class="btn-invite" onclick="openInviteModal('${project.id}')">Invite</button>` : ''}
        </div>
    `;

    return card;
}

function openCreateModal() {
    document.getElementById('create-project-modal').style.display = 'flex';
}

function closeCreateModal() {
    document.getElementById('create-project-modal').style.display = 'none';
    document.getElementById('create-project-form').reset();
    handleWorkspaceSourceChange();
}

async function handleCreateProject(e) {
    e.preventDefault();

    const submitBtn = e.target.querySelector('button[type="submit"]');
    const originalText = submitBtn.textContent;
    submitBtn.textContent = 'Creating...';
    submitBtn.disabled = true;

    const name = document.getElementById('project-name').value;
    const description = document.getElementById('project-description').value;
    const repoOption = document.getElementById('repo-option').value;
    const repoUrlInput = document.getElementById('repo-url');
    const repoUrl = repoUrlInput.value.trim();

    if (repoOption === 'clone' && !repoUrl) {
        alert('Please provide a repository URL to clone.');
        submitBtn.textContent = originalText;
        submitBtn.disabled = false;
        repoUrlInput.focus();
        return;
    }

    console.log('Creating project:', { name, description });

    try {
        const response = await fetch('/api/projects', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                name,
                description,
                repo_option: repoOption,
                repo_url: repoUrl || undefined
            })
        });

        console.log('Response status:', response.status);

        if (response.ok) {
            const data = await response.json();
            console.log('Project created:', data);
            closeCreateModal();
            await loadProjects();
        } else {
            const error = await response.text();
            console.error('Server error:', error);
            alert('Failed to create project: ' + error);
        }
    } catch (err) {
        console.error('Failed to create project:', err);
        alert('Failed to create project: ' + err.message);
    } finally {
        submitBtn.textContent = originalText;
        submitBtn.disabled = false;
    }
}

function handleWorkspaceSourceChange() {
    const repoOption = document.getElementById('repo-option');
    const repoUrlGroup = document.getElementById('repo-url-group');
    const repoUrlInput = document.getElementById('repo-url');

    if (!repoOption || !repoUrlGroup || !repoUrlInput) {
        return;
    }

    const shouldShowRepoUrl = repoOption.value === 'clone';
    repoUrlGroup.style.display = shouldShowRepoUrl ? 'block' : 'none';
    repoUrlInput.required = shouldShowRepoUrl;

    if (!shouldShowRepoUrl) {
        repoUrlInput.value = '';
    }
}

async function enterProject(projectId) {
    window.location.href = `/project?id=${projectId}`;
}

async function openInviteModal(projectId) {
    currentInviteProject = projectId;

    try {
        const response = await fetch(`/api/projects/${projectId}/invite`, {
            method: 'POST'
        });

        if (response.ok) {
            const data = await response.json();
            const inviteUrl = `${window.location.origin}/invite/${data.code}`;
            document.getElementById('invite-link').value = inviteUrl;
            document.getElementById('invite-modal').style.display = 'flex';
        } else {
            alert('Failed to generate invite link');
        }
    } catch (err) {
        console.error('Failed to generate invite:', err);
        alert('Failed to generate invite link');
    }
}

function closeInviteModal() {
    document.getElementById('invite-modal').style.display = 'none';
    currentInviteProject = null;
}

function copyInviteLink() {
    const input = document.getElementById('invite-link');
    input.select();
    document.execCommand('copy');
    alert('Invite link copied to clipboard!');
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatDate(dateStr) {
    if (!dateStr) return 'N/A';
    const date = new Date(dateStr);
    const now = new Date();
    const diff = now - date;
    const days = Math.floor(diff / (1000 * 60 * 60 * 24));

    if (days === 0) return 'Created today';
    if (days === 1) return 'Created yesterday';
    if (days < 7) return `Created ${days} days ago`;
    return `Created on ${date.toLocaleDateString()}`;
}

document.addEventListener('click', (e) => {
    if (e.target.classList.contains('modal')) {
        e.target.style.display = 'none';
    }
});
