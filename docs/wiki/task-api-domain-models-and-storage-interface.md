---
{
  "title": "Task API Domain Models and Storage Interface",
  "summary": "Defines the core domain types — Task, User, Team — and the TaskStore persistence interface for a task management API. Establishes constants for task statuses and priority levels shared across the entire service layer.",
  "concepts": [
    "Task",
    "User",
    "Team",
    "TaskStore",
    "domain model",
    "persistence interface",
    "status constants",
    "priority levels",
    "crypto/rand",
    "ID generation",
    "IsOverdue",
    "JSON serialization"
  ],
  "categories": [
    "domain models",
    "Go API",
    "data layer",
    "task management"
  ],
  "source_docs": [
    "6ec8775206db89ef"
  ],
  "backlinks": null,
  "word_count": 463,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`models.go` is the single source of truth for the task management domain. It defines three entity types, two constant groups, a persistence interface, and a utility method — all the vocabulary the rest of the service builds on.

## Entity Types

### Task

The central work item. Every field that can be absent uses `omitempty` in its JSON tag (`assignee_id`, `labels`, `due_date`), which keeps API responses lean when optional data is missing. `CreatedAt` and `UpdatedAt` are `time.Time` values, meaning serialization is RFC 3339 by default — no ambiguous epoch integers.

```go
type Task struct {
    ID          string    `json:"id"`
    Title       string    `json:"title"`
    Status      string    `json:"status"`
    Priority    int       `json:"priority"`
    AssigneeID  string    `json:"assignee_id,omitempty"`
    DueDate     time.Time `json:"due_date,omitempty"`
    // ...
}
```

The `IsOverdue()` method encapsulates the business rule: a task is overdue when its due date is set, it is in the past, and the task is not yet closed or done. Callers never re-implement that logic.

### User and Team

`User` models a team member with a role field for future authorization checks. `Team` holds a list of member IDs rather than embedded User objects, avoiding circular nesting and keeping payloads flat.

## Constants

Status and priority values are plain typed constants rather than `iota` enums. String statuses (`"open"`, `"in_progress"`, etc.) serialize naturally to JSON without a custom marshaler. Integer priorities (1–4) sort numerically, which simplifies `ORDER BY priority DESC` queries without mapping code.

## TaskStore Interface

`TaskStore` is deliberately narrow: `ListTasks`, `GetTask`, `CreateTask`, `UpdateTask`, `DeleteTask`. This interface exists so the HTTP handler layer can depend on an abstraction rather than a concrete storage implementation, enabling in-memory stores for tests and SQL/NoSQL backends in production without changing handler code.

```go
type TaskStore interface {
    ListTasks() ([]*Task, error)
    GetTask(id string) (*Task, error)
    CreateTask(task *Task) error
    UpdateTask(task *Task) error
    DeleteTask(id string) error
}
```

## ID Generation

`generateID()` uses `crypto/rand` rather than `math/rand`. This matters: math/rand IDs are predictable given knowledge of the seed, which would let an attacker enumerate task IDs. Crypto-random IDs prevent enumeration attacks against the REST API.

## Data Flow

Creation path: HTTP handler calls `CreateTask` → store assigns a `generateID()` value → sets `CreatedAt`/`UpdatedAt` → persists. Read path: handler calls `GetTask(id)` → store returns a `*Task` → handler serializes to JSON. The interface boundary means all timestamp and ID logic stays in the model or store, not scattered in handler code.

## Known Gaps

- `Status` is a plain `string` with no validation on the model layer. Nothing prevents saving `"typo"` as a status; enforcement must happen in the handler or store.
- Priority is an `int` with no range check in the type definition — values outside 1–4 are silently accepted.
- `generateID()` is unexported and returns a fixed-length hex string; the exact length and collision probability are not documented.