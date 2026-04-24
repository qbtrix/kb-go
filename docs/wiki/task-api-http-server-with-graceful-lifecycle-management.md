---
{
  "title": "Task API HTTP Server with Graceful Lifecycle Management",
  "summary": "Implements the HTTP server for the task management API, wiring routes, middleware, and graceful shutdown into a single `Server` struct. Demonstrates idiomatic Go HTTP patterns including method-based routing, timeout guards, and context-scoped shutdown.",
  "concepts": [
    "HTTP server",
    "graceful shutdown",
    "middleware",
    "route registration",
    "http.ServeMux",
    "ReadTimeout",
    "WriteTimeout",
    "ShutdownTimeout",
    "HealthCheck",
    "NewServer",
    "constructor pattern",
    "context timeout"
  ],
  "categories": [
    "HTTP server",
    "Go API",
    "networking",
    "lifecycle management"
  ],
  "source_docs": [
    "39a4704a179652df"
  ],
  "backlinks": null,
  "word_count": 471,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

`server.go` owns the HTTP transport layer. It keeps all networking concerns — port binding, route registration, middleware ordering, and shutdown sequencing — in one place so the handler and storage layers stay independent of HTTP specifics.

## Server Struct

```go
type Server struct {
    Port    int
    Router  *http.ServeMux
    handler *TaskHandler
    srv     *http.Server
}
```

`handler` and `srv` are unexported. Callers interact with `Start()` and `Shutdown()` only; they cannot reach inside and mutate routing state after construction. `Router` is exported to allow tests to introspect registered routes without starting a real listener.

## Construction and Route Registration

`NewServer` is the single entry point. It accepts a `TaskStore` and a `*Config`, creates the mux and handler internally, calls `registerRoutes()`, and returns a ready-to-start server. This constructor pattern prevents partial initialization — you cannot get a `Server` without all routes registered.

`registerRoutes` uses Go 1.22's method+path syntax (`"GET /tasks"`, `"PUT /tasks/{id}"`), which lets the standard library handle method dispatch and path variable extraction without a third-party router.

## Middleware Chain

```go
Handler: LoggingMiddleware(AuthMiddleware(s.Router))
```

Middleware is applied inside `Start()` rather than in `registerRoutes`. This ordering matters: `AuthMiddleware` wraps the mux so every route requires authentication, then `LoggingMiddleware` wraps that to log both authenticated and rejected requests. Reversing the order would log unauthorized requests before they are rejected but with the authenticated identity already resolved — a subtle information-leak risk.

## Timeout Configuration

Both `ReadTimeout` and `WriteTimeout` are set to 15 seconds. Without these, a slow client that never finishes sending headers can hold a goroutine open indefinitely, eventually exhausting the server's goroutine pool under load. The 15-second value is a practical ceiling for a task API where no response body should take that long to produce.

## Graceful Shutdown

```go
func (s *Server) Shutdown() error {
    ctx, cancel := context.WithTimeout(context.Background(), ShutdownTimeout)
    defer cancel()
    return s.srv.Shutdown(ctx)
}
```

`ShutdownTimeout` (10 seconds) gives in-flight requests time to complete before the process exits. Without a timeout on the shutdown context, a stalled request handler could delay termination indefinitely — a problem in containerized deployments where the orchestrator sends SIGKILL after a deadline.

## Health Endpoint

`HealthCheck` is a top-level function (not a method) so it can be registered without a handler instance and used by load balancers independently of auth middleware. It returns a JSON body with a UTC timestamp, giving operators a quick sanity check that both the process and its clock are healthy.

## Known Gaps

- `Start()` calls `ListenAndServe` directly and returns its error — the caller is responsible for distinguishing `http.ErrServerClosed` (expected after `Shutdown`) from real errors. This is not handled in the example.
- There is no `IdleTimeout` set on the `http.Server`, which means keep-alive connections can linger past the write timeout.
- `DefaultPort` (8080) is a package-level constant; there is no environment variable override path shown in this file.