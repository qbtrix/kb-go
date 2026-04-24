---
{
  "title": "Stop Command: Pause Replication for a Database via IPC",
  "summary": "Implements the `litestream stop` sub-command, which sends a request to a running daemon to pause replication for a specific database. Always waits for the daemon to complete a final sync before acknowledging, ensuring no committed transactions are lost when replication is paused.",
  "concepts": [
    "StopCommand",
    "Unix domain socket",
    "HTTP IPC",
    "StopRequest",
    "StopResponse",
    "final sync guarantee",
    "replication pause",
    "control socket",
    "database lifecycle"
  ],
  "categories": [
    "cli",
    "litestream",
    "ipc"
  ],
  "source_docs": [
    "3cedbc5bdff0c0d5"
  ],
  "backlinks": null,
  "word_count": 319,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`StopCommand` sends a `litestream.StopRequest` to a running daemon via HTTP over the Unix control socket. It is the complement to `StartCommand` and is used when an operator needs to temporarily pause replication for a specific database — for example, before a schema migration that would generate many small transactions.

## Guaranteed Final Sync

According to the usage text, `stop` "always waits for shutdown and final sync." This is a critical safety guarantee: if replication were paused mid-transaction, the replica could end up in a state where it has written some but not all of the LTX file for a transaction, making the replica inconsistent. By waiting for a final sync before returning, the command ensures the replica reflects all committed transactions up to the point of the stop.

This guarantee is enforced by the daemon's `/stop` handler, not by the CLI. The CLI simply blocks on the HTTP response, which the daemon does not send until the sync is complete.

## IPC Pattern

Identical to `start.go` and `register.go`:

1. Validate arguments (exactly one database path, positive timeout)
2. Construct an `http.Client` with Unix socket transport
3. POST `litestream.StopRequest{Path, Timeout}` to `http://localhost/stop`
4. Parse `litestream.StopResponse` and pretty-print as JSON

The `Timeout` field is sent in the request body so the daemon knows how long the client is willing to wait. This prevents a mismatch where the daemon waits 60 seconds for a final sync but the client's HTTP timeout fires at 30 seconds, leaving the daemon performing unnecessary work after the client has given up.

## Error Handling

Same two-level error response handling as `register.go` and `start.go`: try `litestream.ErrorResponse` first, fall back to raw body. This handles cases where an HTTP proxy or the OS returns an error body in a non-JSON format.

## Known Gaps

No dedicated test file for `StopCommand`. Behavior is covered indirectly through `TestSyncCommand_Run` and integration tests that exercise the IPC server's stop endpoint.