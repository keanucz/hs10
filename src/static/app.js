let ws = null;
let reconnectInterval = null;
const projectId = "default";

const messagesArea = document.getElementById("messages");
const messageForm = document.getElementById("message-form");
const messageInput = document.getElementById("message-input");

function connectWebSocket() {
    const protocol = window.location.protocol === "https:" ? "wss:" : "ws:";
    const wsUrl = `${protocol}//${window.location.host}/ws?projectId=${projectId}`;

    ws = new WebSocket(wsUrl);

    ws.onopen = () => {
        console.log("WebSocket connected");
        clearInterval(reconnectInterval);
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
    };

    ws.onclose = () => {
        console.log("WebSocket disconnected");
        addSystemMessage("Disconnected from server. Reconnecting...");
        reconnectInterval = setInterval(() => {
            connectWebSocket();
        }, 3000);
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
    avatarEl.textContent = message.senderType === "user" ? "U" : "A";

    const contentEl = document.createElement("div");
    contentEl.className = "message-content";

    const senderEl = document.createElement("div");
    senderEl.className = "message-sender";
    senderEl.textContent = message.senderType === "user" ? window.userData.username : "Agent";

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

messageForm.addEventListener("submit", (e) => {
    e.preventDefault();

    const content = messageInput.value.trim();
    if (!content) return;

    sendMessage(content);
    messageInput.value = "";
});

connectWebSocket();
