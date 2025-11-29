# Development Guide

Quick reference for developing ReplyChat.

## Quick Start

```bash
# Install dependencies
make deps

# Run in development mode
make dev

# Build and run
make run

# Build only
make build
./replychat
```

## Project Structure

```
replychat/
├── src/
│   ├── main.go              # HTTP server, WebSocket, routing, database
│   ├── agents/              # Agent processing logic
│   │   └── processor.go     # Keyword detection, responses, task creation
│   ├── template/            # HTML templates (embedded at compile time)
│   │   ├── index.html       # Landing/login page
│   │   └── project.html     # Main chat workspace
│   └── static/              # Static assets (embedded at compile time)
│       ├── styles.css       # Application styling
│       └── app.js           # WebSocket client, UI interactions
├── data/                    # SQLite database (auto-created)
└── go.mod                   # Dependencies
```

## Key Files Explained

### src/main.go
- HTTP server setup with standard library
- WebSocket hub for real-time communication
- Database initialization and schema creation
- Session management with cookies
- Route handlers for auth and pages
- Graceful shutdown handling

### src/agents/processor.go
- Keyword-based agent triggering
- Simulated agent responses (placeholder for Claude API)
- Task/issue proposal logic
- Message broadcasting to WebSocket clients

### src/template/*.html
- Go template syntax: `{{.FieldName}}`
- Server-rendered pages with data injection
- Embedded at compile time using `//go:embed`

### src/static/*
- CSS and JavaScript files
- Served at `/static/*` route
- Embedded at compile time

## Development Workflow

### Making Changes

**Backend changes (Go code):**
```bash
# Option 1: Auto-reload with air (recommended)
go install github.com/cosmtrek/air@latest
air

# Option 2: Manual reload
make dev
# Make changes, Ctrl+C, make dev again
```

**Frontend changes (HTML/CSS/JS):**
- Edit files in `src/template/` or `src/static/`
- Restart server (embedded files are read at compile time)
- For faster iteration, temporarily serve static files from disk

**Database changes:**
- Edit `createTables()` in `src/main.go`
- Run `make clean` to remove old database
- Restart server to create new schema

### Adding Features

**New route:**
```go
// In main() function
mux.HandleFunc("/your-route", yourHandler)

func yourHandler(w http.ResponseWriter, r *http.Request) {
    // Your logic
}
```

**New agent type:**
```go
// In src/agents/processor.go
keywords := map[string]string{
    "your_keyword": "your_agent_type",
}

responses := map[string]string{
    "your_agent_type": "Agent response text",
}

agentNames := map[string]string{
    "your_agent_type": "Agent Display Name",
}
```

**New database table:**
```go
// In createTables() function
{
    name: "your_table",
    query: `CREATE TABLE IF NOT EXISTS your_table (
        id TEXT PRIMARY KEY,
        field1 TEXT,
        field2 INTEGER,
        created_at TIMESTAMP
    )`,
}
```

**New WebSocket message type:**
```go
// In handleMessage() switch statement
case "your.message.type":
    handleYourMessage(c, msg)
```

## Testing

### Manual Testing
```bash
# Start server
make dev

# Open browser
open http://localhost:8080

# Login with any name/email
# Send messages to trigger agents
```

### Database Inspection
```bash
sqlite3 data/tables.db
sqlite> .tables
sqlite> .schema messages
sqlite> SELECT * FROM messages LIMIT 10;
sqlite> .quit
```

### WebSocket Testing
```bash
# Use websocat
brew install websocat
websocat ws://localhost:8080/ws?projectId=default
```

## Common Tasks

### Reset Database
```bash
make clean
make dev
```

### View Logs
```bash
# Server logs go to stdout
# Format: 2025/01/01 12:00:00 file.go:123: message
```

### Change Port
```bash
# Edit .env
PORT=3000

# Or set environment variable
PORT=3000 make dev
```

### Add Environment Variable
```bash
# Edit .env.example (for documentation)
YOUR_VAR=default_value

# Edit .env (your local config)
YOUR_VAR=actual_value

# Use in code
val := os.Getenv("YOUR_VAR")
```

## Debugging

### Enable Verbose Logging
```go
// In main.go
log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
```

### Debug WebSocket Messages
```javascript
// In app.js, add to ws.onmessage
console.log("Received:", data);
```

### Inspect Embedded Files
```bash
# List embedded files
go list -f '{{.EmbedFiles}}' ./src
```

### Check Binary Size
```bash
ls -lh replychat

# Reduce size with stripping
go build -ldflags="-s -w" -o replychat ./src
```

## Architecture Patterns Used

### Embedded Filesystem
- Templates and static files bundled in binary
- Single file deployment
- Use `//go:embed` directive

### Hub Pattern (WebSocket)
- Central hub manages all connected clients
- Channels for register, unregister, broadcast
- Thread-safe with mutex for client map

### Repository Pattern
- Database queries in handler functions
- Consider extracting to separate package for larger apps

### Session-Based Auth
- Cookie stores session ID
- Database lookup on each request
- No JWT/OAuth for simplicity

### Template Rendering
- Pre-parse all templates at startup
- Cache in memory for fast rendering
- Pass structs to templates for type safety

## Performance Tips

### Database
- SQLite WAL mode enabled for better concurrency
- Use prepared statements for repeated queries
- Index frequently queried columns

### WebSocket
- Use channels for non-blocking sends
- Limit broadcast message size
- Consider message batching for high frequency

### Templates
- Templates parsed once at startup
- Avoid parsing on each request
- Keep template logic simple

### Memory
- Use connection pools wisely
- Close resources in defer statements
- Profile with pprof if needed

## Production Checklist

- [ ] Set `ANTHROPIC_API_KEY` for real AI integration
- [ ] Use HTTPS (reverse proxy recommended)
- [ ] Configure proper CORS if needed
- [ ] Implement rate limiting
- [ ] Add request logging middleware
- [ ] Set up database backups
- [ ] Monitor WebSocket connection count
- [ ] Add health check endpoint
- [ ] Configure proper timeouts
- [ ] Use process manager (systemd, supervisor)
- [ ] Set up log rotation
- [ ] Configure firewall rules

## Troubleshooting

**Binary won't build:**
```bash
go mod tidy
go clean -cache
go build ./src
```

**Port already in use:**
```bash
# Find and kill process
lsof -ti:8080 | xargs kill -9

# Or change port
PORT=8081 make dev
```

**WebSocket won't connect:**
- Check browser console for errors
- Verify server is running
- Check firewall settings
- Ensure cookie/session is valid

**Database locked:**
- Only one writer at a time with SQLite
- Increase busy_timeout
- Consider PostgreSQL for high concurrency

**Templates not updating:**
- Remember: templates are embedded at compile time
- Must rebuild binary to see changes
- Use `make dev` for quick iteration

## Resources

- [Go net/http docs](https://pkg.go.dev/net/http)
- [Gorilla WebSocket](https://github.com/gorilla/websocket)
- [Go templates](https://pkg.go.dev/html/template)
- [SQLite WAL mode](https://www.sqlite.org/wal.html)
- [OpenAI API](https://platform.openai.com/docs/)
