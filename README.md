# ReplyChat

Multi-User AI Agent Collaboration Platform built with Go and WebSockets.

## Project Structure

```
replychat/
├── src/
│   ├── main.go              # HTTP server, WebSocket handling, routing
│   ├── agents/              # AI agent processing package
│   │   └── processor.go     # Message processing and agent responses
│   ├── template/            # HTML templates (embedded)
│   │   ├── index.html       # Landing page
│   │   └── project.html     # Main chat interface
│   └── static/              # Static assets (embedded)
│       ├── styles.css       # Application styles
│       └── app.js           # WebSocket client and UI logic
├── data/                    # SQLite database directory
│   └── tables.db            # SQLite database (auto-created)
├── go.mod                   # Go module definition
├── Dockerfile               # Container build instructions
├── docker-compose.yml       # Docker orchestration
├── .env.example             # Environment variables template
└── README.md                # This file
```

## Technology Stack

**Backend:**

- Go 1.24 with standard library HTTP server
- Gorilla WebSocket for real-time communication
- SQLite with WAL mode for persistence
- Embedded templates and static files

**Frontend:**

- Vanilla JavaScript with WebSocket client
- Pico.css (classless, lightweight CSS) layered with custom Space Grotesk theming
- Server-rendered HTML templates and reusable static assets
- Optional experimental landing pages under `static/experiments/`

**Database:**

- SQLite with WAL mode
- Schema includes: users, sessions, projects, messages, agents, issues, artifacts

## Features

### Current (v0.1)

- ✅ **Real-time chat** - WebSocket-based multi-user chat
- ✅ **OpenAI GPT integration** - Agents powered by GPT-4o-mini
- ✅ **@mention system** - Type `@pm`, `@backend`, `@frontend` with autocomplete
- ✅ **3 AI agents** - Product Manager, Backend Architect, Frontend Developer
- ✅ **Session management** - Simple email/name authentication
- ✅ **Message persistence** - SQLite database with WAL mode
- ✅ **Single binary** - Embedded templates and static assets
- ✅ **Smooth scrolling** - Styled scrollbar and chat area
- ✅ **Project workspaces** - Every project gets an isolated `data/projects/<project-id>` directory
- ✅ **Git bootstrap** - Choose between initializing a repo or cloning an existing remote during project creation
- ✅ **Agent file editing** - Agents apply JSON action plans directly to the filesystem using native Go APIs

### Coming Soon (See FUTURE-DEVELOPMENT.md)

- 🔄 **Kanban board** - Visual task management
- 🔄 **Autonomous agents** - Agents pick up and execute tasks automatically
- 🔄 **Git upstream sync** - Agents push commits and manage branches
- 🔄 **Workspace diff viewer** - Inspect and approve agent changes before they land

## Setup Instructions

### Prerequisites

- Go 1.24 or higher
- Docker (optional, for containerized deployment)

### Local Development

1. Clone the repository and navigate to the project:

  ```bash
  cd replychat
  ```

1. Create environment file:

  ```bash
  cp .env.example .env
  ```

1. Edit `.env` and add your configuration:

  ```bash
  PORT=8080
  OPENAI_API_KEY=your_key_here
  ```

1. Download dependencies:

  ```bash
  go mod download
  ```

1. Run the application:

  ```bash
  go run ./src
  ```

1. Open browser to `http://localhost:8080`

### Docker Deployment

1. Build and run with Docker Compose:

  ```bash
  docker-compose up --build
  ```

1. Access at `http://localhost:8080`

### Building Binary

```bash
CGO_ENABLED=0 go build -o replychat ./src
./replychat
```

## Usage

### Getting Started

1. Open `http://localhost:8080` in your browser
2. Enter your name and email to log in
3. You'll land on the Projects dashboard where you can open an existing workspace or create a new one

### Creating a Project Workspace

1. Click **+ Create New Project** on the Projects dashboard
1. Enter the project name and an optional description
1. Choose the workspace source:

    - **Initialize an empty Git repository** – creates `data/projects/<project-id>` and runs `git init`
    - **Clone an existing repository** – provide any HTTPS/SSH remote; the server clones it into the workspace directory

1. Submit the form to create the project; the API responds with the workspace path for logging

Workspaces live on disk under `data/projects/`. Agents never leave this directory tree thanks to secure path joining. The agent action plans (see `OPENAI_INTEGRATION.md`) create and mutate files directly in these folders, so you can open them in your editor or run `git status` immediately after an agent responds.

### Design System & Landing Page Experiments

- `template/index.html` now features a cinematic hero, workflow timeline, testimonial grid, and CTA banner styled with Pico.css and custom CSS variables.
- Authenticated experiences (`projects.html`, `project.html`, `kanban.html`) share the same design language: Space Grotesk typography, glass panels, gradients, and refreshed buttons/cards.
- Compare lightweight CSS frameworks by visiting `http://localhost:8080/static/experiments/landing-chota.html`, which uses the sub-7KB [Chota](https://jenil.github.io/chota) framework. Duplicate that file to prototype additional looks from the [awesome-css-frameworks](https://github.com/troxler/awesome-css-frameworks) list.

### Chat Interface

- Type messages in the input box at the bottom
- Messages are broadcast to all connected users in real-time
- AI agents respond based on keyword detection

### Using AI Agents

**@Mention (Recommended):**

- `@pm` - Product Manager
- `@backend` - Backend Architect
- `@frontend` - Frontend Developer

Type `@` to see autocomplete dropdown. Use arrow keys or click to select.

**Keyword Triggers (Fallback):**

- "requirement", "feature", "need" → Product Manager
- "api", "backend", "database" → Backend Architect
- "ui", "frontend", "component" → Frontend Developer

### WebSocket Protocol

Messages use JSON format:

**Client → Server:**

```json
{
  "type": "chat.message",
  "payload": {
    "projectId": "default",
    "content": "message text"
  }
}
```

**Server → Client:**

```json
{
  "type": "message.received",
  "payload": {
    "message": {
      "id": "uuid",
      "projectId": "default",
      "senderId": "user_id",
      "senderType": "user",
      "content": "message text",
      "messageType": "chat",
      "timestamp": "2025-01-01T00:00:00Z"
    }
  }
}
```

## Database Schema

### Core Tables

**users:**

- id (TEXT, primary key)
- email (TEXT, unique, not null)
- name (TEXT, not null)
- avatar (TEXT)
- created_at (TIMESTAMP)

**sessions:**

- id (TEXT, primary key)
- user_id (TEXT, foreign key)
- created_at (TIMESTAMP)

**projects:**

- id (TEXT, primary key)
- name (TEXT, not null)
- description (TEXT)
- owner_id (TEXT, foreign key)
- settings (TEXT, JSON)
- created_at (TIMESTAMP)

**messages:**

- id (TEXT, primary key)
- project_id (TEXT, foreign key)
- sender_id (TEXT)
- sender_type (TEXT: user/agent)
- content (TEXT)
- message_type (TEXT)
- metadata (TEXT, JSON)
- timestamp (TIMESTAMP)

**agents:**

- id (TEXT, primary key)
- project_id (TEXT, foreign key)
- name (TEXT)
- specialization (TEXT)
- status (TEXT: idle/working/waiting)
- current_task_id (TEXT)
- config (TEXT, JSON)
- created_at (TIMESTAMP)

**issues:**

- id (TEXT, primary key)
- project_id (TEXT, foreign key)
- title (TEXT)
- description (TEXT)
- priority (TEXT: urgent/high/medium/low)
- status (TEXT: proposed/todo/inProgress/review/done)
- created_by (TEXT)
- created_by_type (TEXT)
- assigned_agent_id (TEXT)
- queued_at, started_at, completed_at (TIMESTAMP)
- tags (TEXT, JSON)

**artifacts:**

- id (TEXT, primary key)
- issue_id (TEXT, foreign key)
- type (TEXT: code/schema/design/document)
- title (TEXT)
- content (TEXT)
- language (TEXT)
- version (INTEGER)
- created_by (TEXT)
- approved_by (TEXT)
- approved_at (TIMESTAMP)
- created_at (TIMESTAMP)

## Extending the System

### Adding New Agents

Edit `src/agents/processor.go`:

1. Add keywords to the `keywords` map:

   ```go
   keywords := map[string]string{
     "your_keyword": "your_agent_type",
   }
   ```

1. Add response template:

   ```go
   responses := map[string]string{
     "your_agent_type": "Your agent response",
   }
   ```

1. Add agent name:

   ```go
   agentNames := map[string]string{
     "your_agent_type": "Your Agent Name",
   }
   ```

### Adding API Routes

Edit `src/main.go` in the `main()` function:

```go
mux.HandleFunc("/your-route", yourHandler)
```

### Adding Database Tables

Edit `src/main.go` in the `createTables()` function:

```go
{
    name: "your_table",
    query: `CREATE TABLE IF NOT EXISTS your_table (
        id TEXT PRIMARY KEY,
        ...
    )`,
}
```

## Architecture Patterns

### Embedded Assets

Static files and templates are embedded into the binary using `//go:embed`:

```go
//go:embed template/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS
```

### WebSocket Hub

The Hub pattern manages connected clients and message broadcasting:

- `register` channel: Add new clients
- `unregister` channel: Remove clients
- `broadcast` channel: Send messages to all clients

### Session Management

Cookie-based sessions with database lookup:

1. User logs in → session ID created
2. Session ID stored in HTTP-only cookie
3. Each request validates session against database

### Graceful Shutdown

Signal handling ensures clean shutdown:

1. Listen for SIGINT/SIGTERM
2. Close HTTP server with timeout
3. Close database connections

## Development Tips

### Hot Reload

Use a tool like `air` for auto-reloading during development:

```bash
go install github.com/cosmtrek/air@latest
air
```

### Database Inspection

```bash
sqlite3 data/tables.db
.schema
SELECT * FROM users;
```

### Logging

All logs include file and line number for debugging:

```go
log.SetFlags(log.LstdFlags | log.Lshortfile)
```

## Production Considerations

- Set `OPENAI_API_KEY` for real AI agent integration
- Use HTTPS in production (reverse proxy recommended)
- Configure proper CORS if hosting frontend separately
- Implement rate limiting for WebSocket connections
- Add database backups for SQLite WAL files
- Monitor WebSocket connection counts
- Implement proper error recovery and retry logic

## Documentation

- **README.md** (this file) - Quick start and basic usage
- **FUTURE-DEVELOPMENT.md** - Detailed roadmap for Kanban, git integration, file editing
- **CHANGELOG.md** - Recent changes and features
- **DEVELOPMENT.md** - Developer guide and troubleshooting
- **OPENAI_INTEGRATION.md** - OpenAI API setup details
- **PROJECT_SUMMARY.md** - Complete project overview

## Resources

- [OpenAI API](https://platform.openai.com/docs/)
- [Gorilla WebSocket](https://github.com/gorilla/websocket)
- [Go Templates](https://pkg.go.dev/html/template)
- [SQLite WAL Mode](https://www.sqlite.org/wal.html)

## License

This is a hackathon project template.
