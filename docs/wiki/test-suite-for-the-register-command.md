---
{
  "title": "Test Suite for the Register Command",
  "summary": "Tests the `register` sub-command against both argument validation and a live litestream server, verifying that databases can be added to a running daemon and that duplicate registrations are handled gracefully. The suite exercises the full Unix socket IPC path including error paths.",
  "concepts": [
    "RegisterCommand",
    "Unix domain socket",
    "litestream.Server",
    "idempotent registration",
    "store.DBs",
    "testingutil.MustOpenDBs",
    "testSocketPath",
    "integration test",
    "argument validation"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "017f138864c24f9c"
  ],
  "backlinks": null,
  "word_count": 305,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file covers `RegisterCommand.Run()` with a mix of argument validation tests (no daemon required) and integration tests that spin up a real `litestream.Server` on a temporary Unix socket.

## Argument Validation Tests

Four subtests confirm that the command fails fast with clear messages when invoked incorrectly:

- **MissingDBPath** — calling with only flags and no positional argument returns `"database path required"`
- **MissingReplicaFlag** — omitting `-replica` returns `"replica URL required (use -replica flag)"`
- **TooManyArguments** — passing two positional arguments returns `"too many arguments"`
- **InvalidTimeoutZero** — `-timeout 0` returns `"timeout must be greater than 0"`
- **SocketConnectionError** — a valid invocation with a non-existent socket path returns a connection error

These tests run without any daemon, confirming that pre-flight validation happens before any I/O.

## Integration Tests

The **Success** subtest spins up a minimal store, wraps it in a `litestream.Server`, starts it on a temp socket path, and then calls `RegisterCommand.Run()` with real arguments. After the call, it verifies that `store.DBs()` contains exactly one entry — confirming the daemon registered the database.

The **AlreadyExists** subtest pre-populates the store with the database, then registers it again. The store still contains exactly one database afterward, confirming idempotency. This prevents a failure mode where a provisioning script registers the same database twice and ends up with duplicated replication loops.

## Test Infrastructure

- `testingutil.MustOpenDBs()` creates a temporary SQLite database and returns both a `litestream.DB` and a `*sql.DB`
- `testSocketPath(t)` returns a unique temporary socket path for each test, preventing socket file collisions between parallel tests
- The server and store are opened and deferred-closed in each subtest to ensure cleanup even on failure

## Known Gaps

No tests for the JSON output format of a successful registration response. If `RegisterDatabaseResponse` gains new fields, there is no assertion that they appear in the printed output.