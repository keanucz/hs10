let ws = null;
let reconnectTimeout = null;
let isConnecting = false;
let reconnectAttempts = 0;
const MAX_RECONNECT_ATTEMPTS = 10;
let projectId = "default";
const agentCardMap = {};
const dialogCards = {};
let dialogOverlay = null;

const messagesArea = document.getElementById("messages");
const messageForm = document.getElementById("message-form");
const messageInput = document.getElementById("message-input");

const agents = [
    { id: "product_manager", name: "Product Manager", trigger: "@pm" },
    { id: "backend_architect", name: "Backend Architect", trigger: "@backend" },
    { id: "frontend_developer", name: "Frontend Developer", trigger: "@frontend" }
];

let autocompleteVisible = false;
let selectedAutocompleteIndex = 0;

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
    const messageEl = document.createElement("div");
    messageEl.className = `message ${message.senderType}`;

    const avatarEl = document.createElement("div");
    avatarEl.className = "message-avatar";

    if (message.senderType === "user") {
        avatarEl.textContent = "U";
    } else {
        const initials = message.senderName ? message.senderName.split(' ').map(w => w[0]).join('') : "A";
        avatarEl.textContent = initials;
    }

    const contentEl = document.createElement("div");
    contentEl.className = "message-content";

    const senderEl = document.createElement("div");
    senderEl.className = "message-sender";
    senderEl.textContent = message.senderType === "user"
        ? window.userData.username
        : (message.senderName || "Agent");

    const textEl = document.createElement("div");
    textEl.className = "message-text";
    textEl.textContent = message.content;

    contentEl.appendChild(senderEl);
    contentEl.appendChild(textEl);

    messageEl.appendChild(avatarEl);
    messageEl.appendChild(contentEl);

    messagesArea.appendChild(messageEl);
    messagesArea.scrollTop = messagesArea.scrollHeight;
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
        "frontend_developer": "Frontend Developer"
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

messageForm.addEventListener("submit", (e) => {
    e.preventDefault();

    const content = messageInput.value.trim();
    if (!content) return;

    sendMessage(content);
    messageInput.value = "";
    hideAutocomplete();
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
fetchDialogs();
fetchAgentQueues();
fetchAgentStatus();
