let ws = null;
let reconnectTimeout = null;
let isConnecting = false;
let reconnectAttempts = 0;
const MAX_RECONNECT_ATTEMPTS = 10;
let projectId = "default";

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
        case "agent.status":
            updateAgentStatus(data.payload);
            break;
        case "issue.created":
            handleIssueCreated(data.payload);
            break;
        case "dialog.requested":
            handleDialogRequest(data.payload);
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
    console.log("Agent status update:", data);
}

function handleIssueCreated(data) {
    console.log("Issue created:", data);
    addSystemMessage(`New task proposed: ${data.issue.title}`);
}

function handleDialogRequest(data) {
    console.log("Dialog requested:", data);
    addSystemMessage(`Agent ${data.agentId} is asking a question`);
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

connectWebSocket();
