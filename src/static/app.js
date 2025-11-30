let ws = null;
let reconnectTimeout = null;
let isConnecting = false;
let reconnectAttempts = 0;
const MAX_RECONNECT_ATTEMPTS = 10;
let projectId = "default";
const agentCardMap = {};
const dialogCards = {};
let dialogOverlay = null;
const seenMessageIds = new Set();

const messagesArea = document.getElementById("messages");
const messageForm = document.getElementById("message-form");
const messageInput = document.getElementById("message-input");

const agents = [
    { id: "product_manager", name: "Product Manager", trigger: "@pm" },
    { id: "backend_architect", name: "Backend Architect", trigger: "@backend" },
    { id: "frontend_developer", name: "Frontend Developer", trigger: "@frontend" },
    { id: "qa_tester", name: "QA Tester", trigger: "@qa" },
    { id: "devops_engineer", name: "DevOps Engineer", trigger: "@devops" }
];

let autocompleteVisible = false;
let selectedAutocompleteIndex = 0;
let promptCoachState = null;
let promptCoachProcessing = false;

function connectWebSocket() {
    if (isConnecting) {
        console.log("Connection attempt already in progress");
        return;
    }

    if (ws && (ws.readyState === WebSocket.CONNECTING || ws.readyState === WebSocket.OPEN)) {
        console.log("WebSocket already connected or connecting");
        return;
    }

    isConnecting = true;

    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/ws?projectId=${projectId}`;

    console.log("Attempting WebSocket connection...");
    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log("WebSocket connected");
        isConnecting = false;
        reconnectAttempts = 0;
        if (reconnectTimeout) {
            clearTimeout(reconnectTimeout);
            reconnectTimeout = null;
        }
        addSystemMessage("Connected to server");
    };

    ws.onmessage = (event) => {
        try {
            const data = JSON.parse(event.data);
            handleWebSocketMessage(data);
        } catch (err) {
            console.error("Failed to parse message:", err);
        }
    };

    ws.onerror = (error) => {
        console.error("WebSocket error:", error);
        isConnecting = false;
    };

    ws.onclose = () => {
        console.log("WebSocket disconnected");
        isConnecting = false;
        ws = null;

        if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
            addSystemMessage("Failed to reconnect after multiple attempts. Please refresh the page.");
            return;
        }

        if (!reconnectTimeout) {
            reconnectAttempts++;
            addSystemMessage(`Disconnected from server. Reconnecting (attempt ${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})...`);
            reconnectTimeout = setTimeout(() => {
                reconnectTimeout = null;
                connectWebSocket();
            }, 3000);
        }
    };
}

function handleWebSocketMessage(data) {
    switch (data.type) {
        case "message.received":
            addMessage(data.payload.message);
            break;
        case "issue.created":
            handleIssueCreated(data.payload);
            break;
        case "issue.updated":
            handleIssueUpdated(data.payload);
            break;
        case "agent.queue":
            handleAgentQueueUpdate(data.payload);
            break;
        case "dialog.requested":
            handleDialogRequest(data.payload);
            break;
        case "dialog.responded":
            handleDialogResponded(data.payload);
            break;
        case "agent.status":
            updateAgentStatus(data.payload);
            break;
        default:
            console.log("Unknown message type:", data.type);
    }
}

function addMessage(message) {
    renderMessage(message, true);
}

function renderMessage(message, scrollToBottom) {
    if (!message || seenMessageIds.has(message.id)) {
        return;
    }
    const messageEl = document.createElement("div");
    messageEl.className = `message ${message.senderType}`;

    const avatarEl = document.createElement("div");
    avatarEl.className = "message-avatar";

    if (message.senderType === "user") {
        avatarEl.textContent = "U";
    } else {
        const name = message.senderName || formatAgentName(message.senderId);
        const initials = name ? name.split(' ').map(w => w[0]).join('') : "A";
        avatarEl.textContent = initials;
    }

    const contentEl = document.createElement("div");
    contentEl.className = "message-content";

    const senderEl = document.createElement("div");
    senderEl.className = "message-sender";
    if (message.senderType === "user") {
        senderEl.textContent = message.senderName || window.userData.username;
    } else {
        senderEl.textContent = message.senderName || formatAgentName(message.senderId);
    }

    const textEl = document.createElement("div");
    textEl.className = "message-text";
    textEl.innerHTML = formatMessageContent(message);

    contentEl.appendChild(senderEl);
    contentEl.appendChild(textEl);

    messageEl.appendChild(avatarEl);
    messageEl.appendChild(contentEl);

    messagesArea.appendChild(messageEl);
    if (scrollToBottom) {
        messagesArea.scrollTop = messagesArea.scrollHeight;
    }
    seenMessageIds.add(message.id);
}

function addSystemMessage(text) {
    const messageEl = document.createElement("div");
    messageEl.className = "system-message";

    const textEl = document.createElement("p");
    textEl.textContent = text;

    messageEl.appendChild(textEl);
    messagesArea.appendChild(messageEl);
    messagesArea.scrollTop = messagesArea.scrollHeight;
}

function resolveWorkspacePath(message) {
    const raw = (message.workspacePath || message.metadata?.workspacePath || "").trim();
    if (!raw) {
        return "";
    }
    return formatWorkspaceLabel(raw, message.projectId);
}

function formatWorkspaceLabel(rawPath, projectId) {
    const normalized = rawPath.replace(/\\/g, "/");
    const match = normalized.match(/data\/projects\/([^/]+)(\/.*)?$/);
    if (match) {
        const projectSegment = match[1].slice(0, 8);
        const suffix = (match[2] || "").replace(/^\/+/, "");
        if (suffix) {
            return `workspace:${projectSegment}/${suffix}`;
        }
        return `workspace:${projectSegment}`;
    }
    if (projectId) {
        return `workspace:${projectId}`;
    }
    return normalized;
}

function extractMessageNotes(message) {
    return normalizeNotes(message.notes || message.metadata?.notes);
}

function normalizeNotes(rawValue) {
    if (!rawValue) {
        return [];
    }
    if (typeof rawValue === "string") {
        const trimmed = rawValue.trim();
        return trimmed ? [trimmed] : [];
    }
    if (Array.isArray(rawValue)) {
        return rawValue
            .map((note) => (typeof note === "string" ? note.trim() : ""))
            .filter(Boolean);
    }
    return [];
}

function normalizePathList(items) {
    if (!Array.isArray(items)) {
        return [];
    }
    return items
        .map((item) => {
            if (typeof item === "string") {
                return item.trim();
            }
            if (item && typeof item === "object") {
                return (item.path || item.file || "").trim();
            }
            return "";
        })
        .filter(Boolean);
}

function extractPlanSummary(message) {
    const planSource = message.metadata?.plan || message.plan || {};
    return {
        files: normalizePathList(planSource.files || []),
        mutations: normalizePathList(planSource.mutations || []),
    };
}

function parsePlanFromContent(content) {
    const trimmed = (content || "").trim();
    if (!trimmed) {
        return null;
    }

    let target = trimmed;
    if (!looksLikeJson(trimmed)) {
        if (!(trimmed.startsWith("{") && trimmed.endsWith("}"))) {
            return null;
        }
    }

    try {
        const parsed = JSON.parse(target);
        if (!parsed || typeof parsed !== "object") {
            return null;
        }
        const files = normalizePathList(parsed.files || []);
        const mutations = normalizePathList(parsed.mutations || []);
        const notes = normalizeNotes(parsed.notes);
        if (!files.length && !mutations.length && !notes.length) {
            return null;
        }
        return { files, mutations, notes, consumed: true };
    } catch (err) {
        return null;
    }
}

function dedupeStrings(values) {
    const seen = new Set();
    const result = [];
    values.forEach((value) => {
        const trimmed = (value || "").trim();
        if (!trimmed) {
            return;
        }
        const key = trimmed.toLowerCase();
        if (!seen.has(key)) {
            seen.add(key);
            result.push(trimmed);
        }
    });
    return result;
}

function extractGitInfo(message) {
    const git = message.git || message.metadata?.git;
    if (!git || typeof git !== "object") {
        return null;
    }
    return git;
}

function shortCommit(commitId) {
    const value = (commitId || "").trim();
    if (!value) {
        return "unknown";
    }
    return value.length > 7 ? value.slice(0, 7) : value;
}

function renderGitSummary(gitInfo) {
    const commitLabel = shortCommit(gitInfo.commitId || "");
    const branch = gitInfo.branch || "HEAD";
    let status;
    if (gitInfo.pushed) {
        status = `Pushed to origin/${escapeHtml(branch)}`;
    } else if ((gitInfo.remote || "").trim()) {
        status = `Commit on ${escapeHtml(branch)} (push pending)`;
    } else {
        status = `Commit on ${escapeHtml(branch)} (no remote)`;
    }

    return `
        <div class="message-git">
            <div class="git-title">Git</div>
            <div class="git-row">
                <span class="badge badge-dark">${escapeHtml(commitLabel)}</span>
                <span class="git-branch">@ ${escapeHtml(branch)}</span>
                <span class="git-status">${status}</span>
            </div>
        </div>
    `;
}

function renderPlanSummary(summary) {
    const hasFiles = summary.files.length > 0;
    const hasMutations = summary.mutations.length > 0;
    if (!hasFiles && !hasMutations) {
        return "";
    }

    const counts = [];
    if (hasFiles) {
        counts.push(`${summary.files.length} file${summary.files.length === 1 ? "" : "s"}`);
    }
    if (hasMutations) {
        counts.push(`${summary.mutations.length} mutation${summary.mutations.length === 1 ? "" : "s"}`);
    }

    const sections = [];
    if (hasFiles) {
        const list = summary.files
            .map((file) => `<li><span class="badge">${escapeHtml(file)}</span></li>`)
            .join("");
        sections.push(`
            <div class="plan-section">
                <div class="plan-section-title">Files</div>
                <ul class="plan-list plan-list-chips">${list}</ul>
            </div>
        `);
    }

    if (hasMutations) {
        const list = summary.mutations
            .map((mutation) => `<li><code>${escapeHtml(mutation)}</code></li>`)
            .join("");
        sections.push(`
            <div class="plan-section">
                <div class="plan-section-title">Mutations</div>
                <ul class="plan-list plan-list-mutations">${list}</ul>
            </div>
        `);
    }

    const countBadges = counts
        .map((label) => `<span class="plan-count">${escapeHtml(label)}</span>`)
        .join("");

    return `
        <div class="message-plan">
            <div class="plan-header">
                <div class="plan-title">Workspace Update</div>
                <div class="plan-counts">${countBadges}</div>
            </div>
            ${sections.join("")}
        </div>
    `;
}

function parseWorkspaceSummary(content) {
    if (!content) {
        return null;
    }
    const match = content.match(
        /^.+?\s+updated\s+workspace\s+.+?\s+\(files=\d+,\s*mutations=\d+\)(?:;\s*notes:\s*(.+))?$/i
    );
    if (!match) {
        return null;
    }
    const notesPart = match[1] ? match[1].trim() : "";
    return {
        notes: notesPart ? normalizeNotes(notesPart) : [],
    };
}

function extractCodeBlocks(text) {
    const blocks = [];
    if (!text) {
        return { blocks, remainder: "" };
    }

    const pattern = /```([\w.-]+)?\n([\s\S]*?)```/g;
    let match;
    let lastIndex = 0;
    const remainderPieces = [];

    while ((match = pattern.exec(text)) !== null) {
        const before = text.slice(lastIndex, match.index);
        if (before.trim()) {
            remainderPieces.push(before.trim());
        }
        blocks.push({
            language: (match[1] || "").trim(),
            code: (match[2] || "").trim(),
        });
        lastIndex = pattern.lastIndex;
    }

    const after = text.slice(lastIndex);
    if (after.trim()) {
        remainderPieces.push(after.trim());
    }

    const remainder = remainderPieces.join("\n\n");
    return { blocks, remainder };
}

function renderCodeBlocks(blocks) {
    if (!blocks.length) {
        return "";
    }
    const items = blocks
        .map(
            (block) => `
        <div class="code-panel-block">
            <div class="code-panel-label">${block.language ? escapeHtml(block.language) : "Code"}</div>
            <pre class="message-code code-panel-pre">${escapeHtml(block.code)}</pre>
        </div>
    `
        )
        .join("");

    return `
        <div class="message-code-panel">
            <div class="code-panel-title">Code Snippets</div>
            ${items}
        </div>
    `;
}

function formatMessageContent(message) {
    const segments = [];
    const workspacePath = resolveWorkspacePath(message);
    let planSummary = extractPlanSummary(message);
    const rawContent = (message.content || "").trim();
    const planFromContent = parsePlanFromContent(rawContent);
    const summaryInfo = parseWorkspaceSummary(rawContent);
    const notes = dedupeStrings([
        ...extractMessageNotes(message),
        ...(planFromContent?.notes || []),
        ...(summaryInfo?.notes || []),
    ]);
    const gitInfo = extractGitInfo(message);
    const { blocks: codeBlocks, remainder: strippedContent } = extractCodeBlocks(rawContent);

    if ((!planSummary.files.length && !planSummary.mutations.length) && planFromContent) {
        planSummary = {
            files: planFromContent.files,
            mutations: planFromContent.mutations,
        };
    }

    if (workspacePath) {
        segments.push(`
            <div class="message-meta">
                <span class="meta-label">Workspace</span>
                <span class="meta-value">${escapeHtml(workspacePath)}</span>
            </div>
        `);
    }

    const planMarkup = renderPlanSummary(planSummary);
    if (planMarkup) {
        segments.push(planMarkup);
    }

    if (gitInfo) {
        segments.push(renderGitSummary(gitInfo));
    }

    const shouldRenderContent =
        strippedContent &&
        !(planFromContent && planFromContent.consumed) &&
        !summaryInfo &&
        strippedContent !== "";
    if (shouldRenderContent) {
        segments.push(renderPrimaryContent(strippedContent));
    }

    if (codeBlocks.length) {
        segments.push(renderCodeBlocks(codeBlocks));
    }

    if (notes.length) {
        const listItems = notes.map((note) => `<li>${escapeHtml(note)}</li>`).join("");
        segments.push(`
            <div class="message-notes">
                <div class="notes-title">Notes</div>
                <ul>${listItems}</ul>
            </div>
        `);
    }

    return segments.join("");
}

function renderPrimaryContent(text) {
    if (looksLikeJson(text)) {
        try {
            const parsed = JSON.parse(text);
            const formatted = JSON.stringify(parsed, null, 2);
            return `<pre class="message-code">${escapeHtml(formatted)}</pre>`;
        } catch (err) {
            // fall through to plain text
        }
    }
    return `<p>${escapeHtml(text)}</p>`;
}

function looksLikeJson(text) {
    const first = text[0];
    const last = text[text.length - 1];
    return (
        (first === "{" && last === "}") ||
        (first === "[" && last === "]")
    );
}

function escapeHtml(raw) {
    return raw
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#39;");
}

function updateAgentStatus(data) {
    if (!data || data.projectId !== projectId) {
        return;
    }

    const statuses = data.statuses || [];
    statuses.forEach((stat) => {
        updateAgentCardFromQueue(stat);

        const entry = agentCardMap[stat.agent_id];
        if (!entry || !entry.taskEl) {
            return;
        }
        entry.taskEl.textContent = stat.current_issue_title
            ? `Task: ${stat.current_issue_title}`
            : "No active task";
    });
}

function handleIssueCreated(data) {
    console.log("Issue created:", data);
    addSystemMessage(`New task proposed: ${data.issue.title}`);
}

function handleIssueUpdated(data) {
    if (!data || !data.issue) {
        return;
    }
    const issue = data.issue;
    addSystemMessage(`Task updated: ${issue.title} (${issue.status || 'updated'})`);
}

function handleAgentQueueUpdate(data) {
    if (!data || !data.projectId || data.projectId !== projectId) {
        return;
    }

    const queues = data.queues || [];
    queues.forEach(updateAgentCardFromQueue);
}

function updateAgentCardFromQueue(stat) {
    if (!stat || !stat.agent_id) {
        return;
    }

    const entry = agentCardMap[stat.agent_id];
    if (!entry) {
        return;
    }

    if (entry.queueEl) {
        entry.queueEl.textContent = String(stat.queue_depth || 0);
    }

    if (entry.statusEl) {
        const status = stat.status || (stat.queue_depth > 0 ? "queued" : "idle");
        entry.statusEl.textContent = formatAgentStatus(status);
        entry.statusEl.classList.remove("idle", "working", "waiting", "queued");
        entry.statusEl.classList.add(status);
    }
}

function handleDialogRequest(data) {
    if (!data || !data.dialog) {
        return;
    }
    renderDialogCard(data.dialog);
    const agentName = formatAgentName(data.agentId || "agent");
    addSystemMessage(`${agentName} requested input: ${data.dialog.title || "Decision"}`);
}

function handleDialogResponded(data) {
    if (!data || !data.dialog) {
        return;
    }
    removeDialogCard(data.dialog.id);
    const responder = data.dialog.respondedByName || "A teammate";
    const title = data.dialog.title || "dialog";
    const choice = data.dialog.selectedOption || "an option";
    addSystemMessage(`${responder} selected "${choice}" for ${title}.`);
}

function initAgentCards() {
    document.querySelectorAll(".agent-card[data-agent-id]").forEach((card) => {
        const agentId = card.dataset.agentId;
        if (!agentId) {
            return;
        }
        agentCardMap[agentId] = {
            statusEl: card.querySelector(".agent-status"),
            queueEl: card.querySelector(".agent-queue-count"),
            taskEl: card.querySelector(".agent-task"),
        };
    });
}

function initDialogUI() {
    dialogOverlay = document.getElementById("dialog-overlay");
    if (dialogOverlay) {
        dialogOverlay.innerHTML = "";
        dialogOverlay.style.display = "none";
    }
}

function renderDialogCard(dialog) {
    if (!dialogOverlay || !dialog || !dialog.id) {
        return;
    }

    removeDialogCard(dialog.id);
    dialogOverlay.style.display = "flex";

    const card = document.createElement("div");
    card.className = "dialog-card";

    const titleEl = document.createElement("h4");
    titleEl.textContent = dialog.title || `Decision requested by ${formatAgentName(dialog.agentId || "agent")}`;
    card.appendChild(titleEl);

    if (dialog.message) {
        const messageEl = document.createElement("p");
        messageEl.textContent = dialog.message;
        card.appendChild(messageEl);
    }

    const optionsEl = document.createElement("div");
    optionsEl.className = "dialog-options";
    const options = dialog.options && dialog.options.length ? dialog.options : [];

    if (options.length === 0 && dialog.defaultOption) {
        options.push(dialog.defaultOption);
    }

    if (options.length === 0) {
        const waiting = document.createElement("p");
        waiting.textContent = "Waiting for more details...";
        optionsEl.appendChild(waiting);
    } else {
        options.forEach((opt) => {
            const btn = document.createElement("button");
            btn.className = "dialog-option-btn";
            btn.textContent = opt;
            btn.onclick = () => respondToDialog(dialog.id, opt);
            optionsEl.appendChild(btn);
        });
    }

    card.appendChild(optionsEl);
    dialogOverlay.appendChild(card);
    dialogCards[dialog.id] = card;
}

function removeDialogCard(dialogId) {
    if (!dialogId || !dialogCards[dialogId]) {
        return;
    }
    const card = dialogCards[dialogId];
    if (card.parentNode) {
        card.parentNode.removeChild(card);
    }
    delete dialogCards[dialogId];
    if (dialogOverlay && Object.keys(dialogCards).length === 0) {
        dialogOverlay.style.display = "none";
    }
}

function formatAgentStatus(status) {
    switch (status) {
        case "working":
            return "Working";
        case "queued":
        case "waiting":
            return "Queued";
        default:
            return "Idle";
    }
}

function formatAgentName(agentId) {
    const map = {
        "product_manager": "Product Manager",
        "backend_architect": "Backend Architect",
        "frontend_developer": "Frontend Developer",
        "qa_tester": "QA Tester",
        "devops_engineer": "DevOps Engineer"
    };
    return map[agentId] || agentId || "Agent";
}

async function fetchAgentQueues() {
    if (!projectId) {
        return;
    }

    try {
        const response = await fetch(`/api/agent-queues?project_id=${projectId}`);
        if (!response.ok) {
            return;
        }
        const data = await response.json();
        handleAgentQueueUpdate({
            projectId,
            queues: data.queues || [],
        });
    } catch (err) {
        console.error("Failed to load agent queues", err);
    }
}

async function fetchMessages() {
    if (!projectId) {
        return;
    }

    try {
        const response = await fetch(`/api/messages?project_id=${projectId}`);
        if (!response.ok) {
            return;
        }
        const data = await response.json();
        (data.messages || []).forEach((msg) => renderMessage(msg, false));
        messagesArea.scrollTop = messagesArea.scrollHeight;
    } catch (err) {
        console.error("Failed to load messages", err);
    }
}

async function fetchAgentStatus() {
    if (!projectId) {
        return;
    }

    try {
        const response = await fetch(`/api/agent-status?project_id=${projectId}`);
        if (!response.ok) {
            return;
        }
        const data = await response.json();
        updateAgentStatus({ projectId, statuses: data.statuses || [] });
    } catch (err) {
        console.error("Failed to load agent status", err);
    }
}

async function fetchDialogs() {
    if (!projectId) {
        return;
    }

    try {
        const response = await fetch(`/api/dialogs?project_id=${projectId}`);
        if (!response.ok) {
            return;
        }
        const data = await response.json();
        (data.dialogs || [])
            .filter((dialog) => dialog.status === "open")
            .forEach(renderDialogCard);
    } catch (err) {
        console.error("Failed to load dialogs", err);
    }
}

async function respondToDialog(dialogId, option) {
    if (!dialogId) return;

    try {
        const response = await fetch(`/api/dialogs/${dialogId}/respond`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ selected_option: option })
        });

        if (response.ok) {
            const data = await response.json();
            if (data && data.selectedOption && data.title) {
                addSystemMessage(`You selected "${data.selectedOption}" for ${data.title}.`);
            }
            removeDialogCard(dialogId);
        }
    } catch (err) {
        console.error("Failed to respond to dialog", err);
    }
}

function sendMessage(content) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
        addSystemMessage("Not connected to server");
        return;
    }

    const message = {
        type: "chat.message",
        payload: {
            projectId: projectId,
            content: content,
        },
    };

    ws.send(JSON.stringify(message));
}

function createAutocompleteDropdown() {
    const dropdown = document.createElement("div");
    dropdown.id = "autocomplete-dropdown";
    dropdown.className = "autocomplete-dropdown";
    dropdown.style.display = "none";

    agents.forEach((agent, index) => {
        const item = document.createElement("div");
        item.className = "autocomplete-item";
        item.dataset.index = index;

        const trigger = document.createElement("span");
        trigger.className = "autocomplete-trigger";
        trigger.textContent = agent.trigger;

        const name = document.createElement("span");
        name.className = "autocomplete-name";
        name.textContent = agent.name;

        item.appendChild(trigger);
        item.appendChild(name);

        item.addEventListener("click", () => {
            selectAutocompleteItem(index);
        });

        dropdown.appendChild(item);
    });

    messageInput.parentElement.appendChild(dropdown);
    return dropdown;
}

const autocompleteDropdown = createAutocompleteDropdown();
const promptCoachToggle = document.getElementById("prompt-coach-toggle");
const promptCoachCard = document.getElementById("prompt-coach-card");
const promptCoachAnalysis = document.getElementById("prompt-coach-analysis");
const promptCoachSuggestionBlock = document.getElementById("prompt-coach-suggestion-block");
const promptCoachSuggestion = document.getElementById("prompt-coach-suggestion");
const promptCoachAccept = document.getElementById("prompt-coach-accept");
const promptCoachReject = document.getElementById("prompt-coach-reject");
const sendButton = messageForm.querySelector("button[type='submit']");

function showAutocomplete() {
    autocompleteVisible = true;
    selectedAutocompleteIndex = 0;
    autocompleteDropdown.style.display = "block";
    updateAutocompleteSelection();
}

function hideAutocomplete() {
    autocompleteVisible = false;
    autocompleteDropdown.style.display = "none";
}

function updateAutocompleteSelection() {
    const items = autocompleteDropdown.querySelectorAll(".autocomplete-item");
    items.forEach((item, index) => {
        if (index === selectedAutocompleteIndex) {
            item.classList.add("selected");
        } else {
            item.classList.remove("selected");
        }
    });
}

function hidePromptCoachCard() {
    if (!promptCoachCard) {
        return;
    }
    promptCoachCard.classList.add("hidden");
    promptCoachCard.classList.remove("loading");
    if (promptCoachSuggestionBlock) {
        promptCoachSuggestionBlock.classList.add("hidden");
    }
    promptCoachState = null;
    promptCoachProcessing = false;
    messageInput.disabled = false;
    if (sendButton) {
        sendButton.disabled = false;
    }
}

function showPromptCoachLoading() {
    if (!promptCoachCard) {
        return;
    }
    promptCoachCard.classList.remove("hidden");
    promptCoachCard.classList.add("loading");
    if (promptCoachAnalysis) {
        promptCoachAnalysis.textContent = "Clippy is thinking about your prompt…";
    }
    if (promptCoachSuggestionBlock) {
        promptCoachSuggestionBlock.classList.add("hidden");
    }
    if (sendButton) {
        sendButton.disabled = true;
    }
    messageInput.disabled = true;
}

function showPromptCoachSuggestion(state) {
    if (!promptCoachCard) {
        return;
    }
    promptCoachCard.classList.remove("loading");
    if (promptCoachAnalysis) {
        promptCoachAnalysis.textContent = state.analysis || "Clippy polished your prompt.";
    }
    if (promptCoachSuggestionBlock) {
        promptCoachSuggestionBlock.classList.remove("hidden");
    }
    if (promptCoachSuggestion) {
        promptCoachSuggestion.textContent = state.suggestion || state.original;
    }
    if (sendButton) {
        sendButton.disabled = false;
    }
    messageInput.disabled = false;
    messageInput.focus();
}

async function requestPromptCoaching(content) {
    if (!promptCoachToggle) {
        sendMessage(content);
        messageInput.value = "";
        return;
    }
    promptCoachProcessing = true;
    promptCoachState = { original: content };
    showPromptCoachLoading();

    try {
        const response = await fetch("/api/prompt-coach", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ projectId, content })
        });

        if (!response.ok) {
            throw new Error("Prompt coach error");
        }

        const data = await response.json();
        promptCoachState.suggestion = (data.improved_prompt || "").trim();
        promptCoachState.analysis = (data.analysis || "").trim();
        messageInput.value = promptCoachState.suggestion || content;
        showPromptCoachSuggestion(promptCoachState);
    } catch (err) {
        console.error("Prompt coach failed", err);
        addSystemMessage("Clippy is on break, sending your original prompt.");
        hidePromptCoachCard();
        sendMessage(content);
        messageInput.value = "";
    } finally {
        promptCoachProcessing = false;
    }
}

if (promptCoachToggle) {
    const stored = localStorage.getItem("promptCoachEnabled");
    promptCoachToggle.checked = stored === "true";
    promptCoachToggle.addEventListener("change", () => {
        localStorage.setItem("promptCoachEnabled", promptCoachToggle.checked ? "true" : "false");
        if (!promptCoachToggle.checked) {
            hidePromptCoachCard();
        }
    });
}

if (promptCoachAccept) {
    promptCoachAccept.addEventListener("click", () => {
        if (!promptCoachState) {
            hidePromptCoachCard();
            return;
        }
        const content = (messageInput.value || promptCoachState.suggestion || promptCoachState.original || "").trim();
        if (!content) {
            hidePromptCoachCard();
            return;
        }
        sendMessage(content);
        messageInput.value = "";
        hidePromptCoachCard();
    });
}

if (promptCoachReject) {
    promptCoachReject.addEventListener("click", () => {
        if (!promptCoachState) {
            hidePromptCoachCard();
            return;
        }
        const content = promptCoachState.original;
        sendMessage(content);
        messageInput.value = "";
        hidePromptCoachCard();
    });
}

function selectAutocompleteItem(index) {
    const agent = agents[index];
    const value = messageInput.value;
    const atIndex = value.lastIndexOf("@");

    if (atIndex !== -1) {
        messageInput.value = value.substring(0, atIndex) + agent.trigger + " ";
    } else {
        messageInput.value = agent.trigger + " ";
    }

    hideAutocomplete();
    messageInput.focus();
}

messageInput.addEventListener("input", (e) => {
    const value = e.target.value;
    const cursorPos = e.target.selectionStart;

    const textBeforeCursor = value.substring(0, cursorPos);
    const lastAtIndex = textBeforeCursor.lastIndexOf("@");

    if (lastAtIndex !== -1) {
        const textAfterAt = textBeforeCursor.substring(lastAtIndex + 1);

        if (textAfterAt.length === 0 || /^[a-zA-Z]*$/.test(textAfterAt)) {
            showAutocomplete();
        } else {
            hideAutocomplete();
        }
    } else {
        hideAutocomplete();
    }
});

messageInput.addEventListener("keydown", (e) => {
    if (!autocompleteVisible) return;

    if (e.key === "ArrowDown") {
        e.preventDefault();
        selectedAutocompleteIndex = (selectedAutocompleteIndex + 1) % agents.length;
        updateAutocompleteSelection();
    } else if (e.key === "ArrowUp") {
        e.preventDefault();
        selectedAutocompleteIndex = (selectedAutocompleteIndex - 1 + agents.length) % agents.length;
        updateAutocompleteSelection();
    } else if (e.key === "Enter" || e.key === "Tab") {
        if (autocompleteVisible) {
            e.preventDefault();
            selectAutocompleteItem(selectedAutocompleteIndex);
        }
    } else if (e.key === "Escape") {
        hideAutocomplete();
    }
});

document.addEventListener("click", (e) => {
    if (!messageInput.contains(e.target) && !autocompleteDropdown.contains(e.target)) {
        hideAutocomplete();
    }
});

messageForm.addEventListener("submit", async (e) => {
    e.preventDefault();

    if (promptCoachProcessing) {
        return;
    }

    const content = messageInput.value.trim();
    if (!content) return;

    hideAutocomplete();

    if (promptCoachToggle && promptCoachToggle.checked) {
        await requestPromptCoaching(content);
    } else {
        sendMessage(content);
        messageInput.value = "";
        hidePromptCoachCard();
    }
});

// Handle Enter key for textarea (Enter to submit, Shift+Enter for new line)
messageInput.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey) {
        e.preventDefault();
        messageForm.dispatchEvent(new Event("submit"));
    }
});

window.addEventListener("beforeunload", () => {
    if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
        reconnectTimeout = null;
    }
    if (ws) {
        ws.close();
    }
});

if (window.userData && window.userData.projectId) {
    projectId = window.userData.projectId;
}

initAgentCards();
initDialogUI();
connectWebSocket();
fetchMessages();
fetchDialogs();
fetchAgentQueues();
fetchAgentStatus();
