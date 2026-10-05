---
{
  "title": "Test Suite for the Unregister Command",
  "summary": "Tests the `unregister` sub-command including argument validation, socket error handling, idempotent not-found behavior, and successful removal of a tracked database from the store. The not-found idempotency test is the key behavioral contract verified here.",
  "concepts": [
    "UnregisterCommand",
    "idempotent unregister",
    "store.DBs",
    "Unix domain socket",
    "litestream.Server",
    "testingutil.MustOpenDBs",
    "CompactionMonitorEnabled",
    "deprovisioning",
    "argument validation",
    "integration test"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "cd3f90f23cf08f62"
  ],
  "backlinks": null,
  "word_count": 273,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file tests `UnregisterCommand.Run()` with a full set of argument validation cases and two integration tests that spin up a real daemon: one confirming that unregistering a non-existent database succeeds, and one confirming that unregistering a tracked database actually removes it.

## Argument Validation Tests

- **MissingPath** — no positional argument → `"database path required"`
- **TooManyArguments** — two positional arguments → `"too many arguments"`
- **InvalidTimeoutZero** — `-timeout 0` → `"timeout must be greater than 0"`
- **InvalidTimeoutNegative** — `-timeout -1` → `"timeout must be greater than 0"` (same message for both zero and negative)
- **SocketConnectionError** — valid args but non-existent socket → network error

## Integration Tests

### NotFoundIsIdempotent

Creates a store with no databases and starts a server. Calls `unregister` with a path that was never registered (`/nonexistent/db`). Expects no error. This is the idempotency contract: operators should be able to run `unregister` in cleanup scripts without checking first whether the database is currently registered.

### Success

Pre-populates the store with one database, starts a server, calls `unregister` with the database's path, and asserts that `store.DBs()` now returns zero entries. This verifies the full removal path: the daemon's `/unregister` handler must remove the database from the store, not just acknowledge the request.

## Test Infrastructure

- `testingutil.MustOpenDBs()` creates a temporary database with a backing SQL connection
- `testSocketPath(t)` generates a unique temporary socket path per test
- `CompactionMonitorEnabled = false` prevents background goroutines from interfering with the `store.DBs()` assertion

## Known Gaps

No test for a database that is mid-sync when unregister is called. The expected behavior (let the sync finish vs. cancel it) is not verified.