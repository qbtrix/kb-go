---
{
  "title": "Task API HTTP Server Implementation",
  "summary": "The server.go module implements an HTTP server for the task management API with lifecycle management, route registration, and graceful shutdown capabilities. It provides REST endpoints for task CRUD operations, health checks, and includes middleware support for authentication and logging.",
  "concepts": [
    "HTTP Server",
    "REST API",
    "Route Registration",
    "Middleware",
    "Graceful Shutdown",
    "Lifecycle Management",
    "Health Check",
    "CRUD Operations",
    "Context Timeout",
    "Request Handler",
    "ServeMux Router"
  ],
  "categories": [
    "Backend Development",
    "HTTP Server",
    "API Design",
    "Go Standard Library",
    "Server Infrastructure"
  ],
  "source_docs": [
    "39a4704a179652df"
  ],
  "backlinks": null,
  "word_count": 342,
  "compiled_at": "2026-04-07T06:38:55Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Task API HTTP Server

## Overview

The `server.go` module in the `taskapi` package implements a production-ready HTTP server wrapper with comprehensive lifecycle management for the task management API.

## Core Components

### Server Structure

The `Server` struct encapsulates the HTTP server and its dependencies:

```go
type Server struct {
  Port    int                 // Listen port
  Router  *http.ServeMux    // HTTP request router
  handler *TaskHandler      // Task operation handler
  srv     *http.Server      // Underlying HTTP server
}
```

### Constants

- **DefaultPort** (8080): Default HTTP listen port
- **ShutdownTimeout** (10 seconds): Grace period for graceful shutdown

## API Endpoints

The server registers the following REST endpoints via `registerRoutes()`:

| Method | Path | Handler |
|--------|------|----------|
| GET | /tasks | List all tasks |
| POST | /tasks | Create new task |
| GET | /tasks/{id} | Get task by ID |
| PUT | /tasks/{id} | Update task |
| DELETE | /tasks/{id} | Delete task |
| GET | /health | Health check status |

## Key Functions

### NewServer(store, cfg) -> *Server

Factory function that creates and configures a new server instance with all routes registered. Accepts a `TaskStore` implementation and `Config` object.

### Start() -> error

Initiates HTTP server startup with:
- Configured port binding
- Middleware stack (Authentication + Logging)
- Read/Write timeouts set to 15 seconds
- Blocking operation until shutdown

### Shutdown() -> error

Gracefully terminates the server with a configurable timeout (ShutdownTimeout = 10s), allowing in-flight requests to complete.

### HealthCheck(w, r)

Simple health status endpoint returning JSON with current UTC timestamp and "ok" status.

## Middleware Stack

The server applies middleware in order:
1. **AuthMiddleware** - Request authentication
2. **LoggingMiddleware** - Request/response logging

## Dependencies

- `context` - Timeout management for graceful shutdown
- `net/http` - HTTP server and routing
- `fmt`, `log` - Formatting and logging
- `time` - Timeout and timestamp handling

## Design Patterns

- **Dependency Injection**: `TaskStore` and `Config` passed to constructor
- **Lifecycle Management**: Explicit Start/Shutdown methods
- **Middleware Composition**: Chainable middleware wrapping
- **Graceful Shutdown**: Context-based timeout handling