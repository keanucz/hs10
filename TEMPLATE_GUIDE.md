# Foodvibe Template Structure Guide

This document explains the Golang + HTML/JavaScript template pattern used in this project, based on the foodvibe template.

## Core Architecture Pattern

This template uses **Go standard library HTTP server** with embedded static assets, NOT a framework like Fiber or Gin.

### Key Technologies

- **Go net/http**: Standard library HTTP routing
- **embed.FS**: Compile-time embedding of templates and static files
- **html/template**: Server-side template rendering
- **SQLite**: Embedded database with WAL mode
- **WebSocket**: Gorilla WebSocket for real-time communication

## Directory Structure

```
project/
├── src/
│   ├── main.go           # HTTP server entry point
│   ├── package/          # Domain packages (e.g., agents, places)
│   │   └── service.go    # Business logic
│   ├── template/         # HTML templates (embedded)
│   │   └── *.html
│   └── static/           # CSS, JS, images (embedded)
│       └── *.css, *.js
├── data/                 # SQLite database files
├── go.mod                # Go dependencies
├── Dockerfile            # Multi-stage container build
└── docker-compose.yml    # Container orchestration
```

## Pattern Breakdown

### 1. Embedded Assets

Templates and static files are embedded into the binary at compile time:

```go
//go:embed template/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS
```

**Benefits:**
- Single binary deployment
- No need to copy assets separately
- Assets bundled in Docker image automatically

### 2. Template Rendering

Global template cache parsed once at startup:

```go
var parsedTemplates = template.Must(template.ParseFS(templateFS, "template/*.html"))

func renderTemplate(w http.ResponseWriter, name string, data any) error {
    tmpl := parsedTemplates.Lookup(name)
    return tmpl.Execute(w, data)
}
```

**Usage in handlers:**
```go
func handler(w http.ResponseWriter, r *http.Request) {
    data := PageData{Username: "John"}
    renderTemplate(w, "page.html", data)
}
```

### 3. Static File Serving

Create sub-filesystem and serve with prefix stripping:

```go
staticContent, _ := fs.Sub(staticFS, "static")
mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticContent))))
```

**Access in HTML:**
```html
<link rel="stylesheet" href="/static/styles.css">
<script src="/static/app.js"></script>
```

### 4. HTTP Routing

Standard library mux with handler functions:

```go
mux := http.NewServeMux()
mux.HandleFunc("/", indexHandler)
mux.HandleFunc("/login", loginHandler)
mux.HandleFunc("/api/data", apiHandler)
```

**Method checking:**
```go
if r.Method != http.MethodPost {
    http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
    return
}
```

### 5. Database Pattern

SQLite with WAL mode for better concurrency:

```go
db, err := sql.Open("sqlite", "data/tables.db")
db.SetMaxOpenConns(1)  // SQLite requirement
db.Exec(`PRAGMA journal_mode=WAL`)
db.Exec(`PRAGMA synchronous=NORMAL`)
```

**Schema creation:**
```go
func createTables() error {
    tables := []struct {
        name  string
        query string
    }{
        {name: "users", query: `CREATE TABLE IF NOT EXISTS users (...)`},
    }

    for _, tbl := range tables {
        db.Exec(tbl.query)
    }
}
```

### 6. Session Management

Cookie-based sessions with database validation:

```go
// Create session
sessionID := uuid.New().String()
db.Exec(`INSERT INTO sessions (id, user_id) VALUES (?, ?)`, sessionID, userID)
http.SetCookie(w, &http.Cookie{
    Name:     "session_id",
    Value:    sessionID,
    HttpOnly: true,
    MaxAge:   86400 * 7,
})

// Validate session
cookie, _ := r.Cookie("session_id")
var userID string
db.QueryRow(`SELECT user_id FROM sessions WHERE id = ?`, cookie.Value).Scan(&userID)
```

### 7. WebSocket Pattern

Hub-based broadcast system:

```go
type Hub struct {
    clients    map[*Client]bool
    broadcast  chan []byte
    register   chan *Client
    unregister chan *Client
}

func (h *Hub) run() {
    for {
        select {
        case client := <-h.register:
            h.clients[client] = true
        case client := <-h.unregister:
            delete(h.clients, client)
            close(client.send)
        case message := <-h.broadcast:
            for client := range h.clients {
                client.send <- message
            }
        }
    }
}
```

### 8. Configuration

Environment variables with .env file support:

```go
import "github.com/joho/godotenv"

godotenv.Load()  // Load .env (optional)
apiKey := os.Getenv("API_KEY")
```

### 9. Graceful Shutdown

Signal handling for clean shutdown:

```go
shutdownCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

server := &http.Server{Addr: ":8080", Handler: mux}

go func() {
    server.ListenAndServe()
}()

<-shutdownCtx.Done()

ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
server.Shutdown(ctx)
db.Close()
```

### 10. Docker Deployment

Multi-stage build for minimal image size:

```dockerfile
FROM golang:1.24 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app ./src

FROM gcr.io/distroless/base-debian12:latest
COPY --from=builder /app/app /usr/local/bin/app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/app"]
```

## Template Data Flow

1. **User requests page** → HTTP handler
2. **Handler gathers data** → struct with page data
3. **Template rendered** → HTML with interpolated data
4. **Response sent** → Complete HTML page
5. **JavaScript loads** → Fetches additional data via API/WebSocket

## Frontend Integration

**Server-rendered pages:**
- Initial page load uses Go templates
- Data passed as struct to template

**Dynamic updates:**
- WebSocket for real-time updates
- Fetch API for JSON endpoints

**Example:**
```html
<!-- Template variable -->
<h1>Welcome, {{.Username}}</h1>

<!-- JavaScript accesses data -->
<script>
window.userData = {
    userId: "{{.UserID}}",
    username: "{{.Username}}"
};
</script>
```

## Why This Pattern?

**Advantages:**
- Simple deployment (single binary)
- No build tools needed for basic sites
- Fast startup time
- Low memory footprint
- Standard library stability
- Easy to understand and debug

**Best for:**
- Internal tools
- MVPs and prototypes
- Real-time applications
- Server-rendered apps with light JavaScript

**Not ideal for:**
- Heavy client-side SPAs (use separate frontend)
- Complex build pipelines (use Vite, webpack)
- When you need hot module replacement

## Migrating to This Pattern

If you have a separate frontend (React, Vue, etc.), you can:

1. **Option A: Keep separate** - Go serves JSON API, frontend consumes it
2. **Option B: Integrate** - Pre-build frontend, embed dist folder in Go binary
3. **Option C: Hybrid** - Go templates for main pages, JS for interactive components

This template uses **Option C** - server-rendered pages with WebSocket-enhanced interactivity.
