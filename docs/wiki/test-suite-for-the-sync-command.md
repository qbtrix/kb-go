---
{
  "title": "Test Suite for the Sync Command",
  "summary": "Tests the `sync` sub-command against both argument validation and a live litestream server, covering fire-and-forget and blocking wait modes. The integration tests write real SQL data before syncing to ensure the daemon has something to replicate.",
  "concepts": [
    "SyncCommand",
    "CompactionMonitorEnabled",
    "Unix domain socket",
    "litestream.Server",
    "wait mode",
    "integration test",
    "LTX data",
    "testingutil.MustOpenDBs",
    "testSocketPath",
    "no-op sync"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "eb647c4d64b0edc6"
  ],
  "backlinks": null,
  "word_count": 304,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file exercises `SyncCommand.Run()` at two levels: argument validation (no daemon required) and integration tests with a real litestream server on a Unix socket.

## Argument Validation Tests

- **MissingDBPath** — no positional argument → `"database path required"`
- **TooManyArguments** — two positional arguments → `"too many arguments"`
- **InvalidTimeoutZero** — `-timeout 0` → `"timeout must be greater than 0"`
- **SocketConnectionError** — valid args but non-existent socket → network error

## Integration Tests

### Success

Spins up a `litestream.Store` with `CompactionMonitorEnabled = false` (to prevent background compaction from interfering with the test), wraps it in a `litestream.Server`, and starts it on a temp socket. A `CREATE TABLE` statement is executed to create LTX data for the daemon to sync. Then `SyncCommand.Run()` is called and expected to succeed.

Disabling the compaction monitor is important because compaction is asynchronous and could modify the LTX directory between the SQL write and the sync assertion, making the test non-deterministic.

### SuccessWithWait

Identical setup but adds `-wait` to the sync command. This exercises the blocking code path in the daemon's `/sync` handler. Because the test uses a file replica (no actual remote storage), the wait completes quickly — but the test still confirms that the `-wait` flag is accepted and does not cause a timeout or error.

## Why SQL Writes Before Sync

Both integration tests write a `CREATE TABLE` statement before syncing. Without a write, the database WAL may be empty and the sync may be a no-op. A no-op sync still returns success, so the test would pass trivially. Writing data first ensures the daemon actually transfers LTX content, making the test a meaningful end-to-end validation.

## Known Gaps

No test that verifies the sync timeout is respected (i.e., that a slow replica causes the command to time out and return an error).