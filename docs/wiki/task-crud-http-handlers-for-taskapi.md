---
{
  "title": "Task CRUD HTTP Handlers for taskapi",
  "summary": "Implements the five standard CRUD HTTP handlers (List, Create, Get, Update, Delete) for the taskapi package, backed by a TaskStore interface and using shared JSON response helpers. The handler layer is thin by design: it decodes requests, delegates to the store, and encodes responses without embedding business logic.",
  "concepts": [
    "TaskHandler",
    "TaskStore",
    "CRUD",
    "HTTP handlers",
    "dependency injection",
    "CreateRequest",
    "TaskResponse",
    "json.NewDecoder",
    "writeJSON",
    "PathValue",
    "partial update",
    "taskapi"
  ],
  "categories": [
    "http",
    "go",
    "taskapi",
    "api"
  ],
  "source_docs": [
    "366049064f88dda2"
  ],
  "backlinks": null,
  "word_count": 614,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

`handler.go` is the HTTP boundary of the taskapi service. Its role is narrow: translate HTTP requests into store method calls and translate store results (or errors) back into HTTP responses. Business logic — validation rules, status transitions, ownership checks — belongs in the store or a service layer, not here.

## TaskHandler and Dependency Injection

```go
type TaskHandler struct {
    store TaskStore
}

func NewTaskHandler(store TaskStore) *TaskHandler {
    return &TaskHandler{store: store}
}
```

`TaskStore` is an interface (defined elsewhere in the package). `TaskHandler` depends on the interface, not a concrete type, which means tests can supply an in-memory store and production code can supply a database-backed one without changing handler logic. The constructor is the only place the dependency is wired — there are no package-level variables or singletons.

## Request and Response Types

**`CreateRequest`** carries the fields needed to create a task: title, description, optional assignee ID, and priority. JSON tags use `omitempty` on `AssigneeID`, so clients can omit it without sending `null`. The handler constructs the full `Task` from this payload, generating the ID and timestamps internally — callers cannot supply their own ID, which prevents ID-collision attacks or spoofed creation times.

**`TaskResponse`** wraps a `*Task` with an optional `Error` string field tagged `omitempty`. This means success responses serialize as `{"task": {...}}` and error responses as `{"error": "..."}` using the same type, keeping the response schema predictable for clients.

## Handler Implementations

**`List`** reads the optional `status` query parameter and passes it to `store.ListTasks`. The store is responsible for interpreting the filter; the handler applies no default. Errors from the store map to 500.

**`Create`** decodes the request body with `json.NewDecoder(r.Body).Decode`. Malformed JSON produces a 400 with `"invalid request body"`. The handler calls `generateID()` (defined elsewhere) and sets both `CreatedAt` and `UpdatedAt` to `time.Now().UTC()` — using UTC avoids timezone-dependent behavior in storage and sorting.

**`Get`** uses `r.PathValue("id")`, the standard library's Go 1.22+ method for extracting named path segments from a pattern-registered route. Any store error maps to 404 with `"task not found"` — this collapses both "not found" and unexpected errors into the same response, which leaks less information but makes debugging harder.

**`Update`** decodes the request body as `map[string]any` and passes it raw to `store.UpdateTask`. This is a partial-update (PATCH-style) design: only supplied fields are updated, and the store must implement the merge logic. A malformed body produces 400; a missing task produces 404.

**`Delete`** calls `store.DeleteTask` and on success returns `{"deleted": id}` with 200 rather than 204 No Content. Returning the deleted ID in the body is a minor usability choice — clients can confirm which resource was removed.

## Shared Response Helpers

```go
func writeJSON(w http.ResponseWriter, status int, v any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
    writeJSON(w, status, map[string]string{"error": msg})
}
```

Centralizing these two functions ensures every response sets `Content-Type: application/json` before writing the status code. Calling `w.Header().Set` after `w.WriteHeader` would silently drop the header in Go's `http.ResponseWriter`. By convention, `writeJSON` is always called as the final action in a handler branch, making the response-then-return pattern easy to audit.

## Known Gaps

- Store errors in `Get`, `Update`, and `Delete` always return 404 regardless of the actual error type. A database connectivity failure would be incorrectly reported as "task not found" rather than a 500.
- `Create` does not validate required fields (e.g., empty `Title`) before writing to the store.
- `Update` accepts arbitrary `map[string]any` without field whitelisting, so clients could attempt to overwrite internal fields like `created_at` or `id` if the store does not guard against it.
- `json.NewEncoder(w).Encode` appends a trailing newline, which is normally harmless but may surprise consumers that compare raw response bodies byte-for-byte.