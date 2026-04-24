---
{
  "title": "Unregister Command: Remove a Database from a Running Daemon",
  "summary": "Implements the `litestream unregister` sub-command, which instructs a running daemon to stop replicating a specific database and remove it from the active store. The operation is idempotent — unregistering a database that is not tracked succeeds without error.",
  "concepts": [
    "UnregisterCommand",
    "Unix domain socket",
    "UnregisterDatabaseRequest",
    "UnregisterDatabaseResponse",
    "idempotent unregister",
    "litestream.Store",
    "deprovisioning",
    "control socket",
    "database lifecycle",
    "HTTP IPC"
  ],
  "categories": [
    "cli",
    "litestream",
    "ipc"
  ],
  "source_docs": [
    "c4614c2f4edaa53b"
  ],
  "backlinks": null,
  "word_count": 286,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`UnregisterCommand` is the inverse of `RegisterCommand`. It sends a `litestream.UnregisterDatabaseRequest` to the daemon via HTTP over the Unix control socket, asking it to stop monitoring and replicating a specific SQLite database.

## Idempotency

From the test suite (`NotFoundIsIdempotent`), unregistering a database that is not currently tracked by the daemon returns success rather than an error. This design choice makes `unregister` safe to use in deprovisioning scripts that may run even if the database was never registered or was already unregistered. Without idempotency, a second run of the deprovisioning script would fail, requiring error-handling logic in the caller.

## When to Use

`unregister` is appropriate when:
- A tenant's database is being permanently deleted and replication should stop
- A database is being migrated to a different host and should no longer be tracked by the current daemon
- A directory-monitored database has been deleted and the daemon should forget it

After unregistering, any existing replica files remain in storage — `unregister` does not delete remote backup data.

## IPC Pattern

Identical to `register.go`, `start.go`, and `stop.go`:

1. Parse flags (`-timeout`, `-socket`)
2. Validate arguments (one database path, positive timeout)
3. Construct Unix socket HTTP client
4. POST `litestream.UnregisterDatabaseRequest{Path, Timeout}` to `http://localhost/unregister`
5. Parse `litestream.UnregisterDatabaseResponse` and pretty-print as JSON

## Timeout Validation

Like `sync.go`, this command validates that timeout is greater than 0. The `register.go` command has the same validation. This consistency prevents a class of bugs where a zero timeout causes the command to always time out and return a misleading error message.

## Known Gaps

No option to also delete remote replica data as part of unregistration. Users who want to clean up storage must do so separately via the storage provider's tools.