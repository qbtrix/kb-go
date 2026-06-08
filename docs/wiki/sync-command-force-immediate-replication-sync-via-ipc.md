---
{
  "title": "Sync Command: Force Immediate Replication Sync via IPC",
  "summary": "Implements the `litestream sync` sub-command, which asks a running daemon to immediately sync a database's WAL to its replicas rather than waiting for the next scheduled sync interval. Supports an optional blocking mode that waits for remote replication to complete.",
  "concepts": [
    "SyncCommand",
    "force sync",
    "Unix domain socket",
    "SyncRequest",
    "SyncResponse",
    "wait mode",
    "remote replication",
    "HTTP IPC",
    "deployment sync",
    "timeout validation"
  ],
  "categories": [
    "cli",
    "litestream",
    "ipc"
  ],
  "source_docs": [
    "4b1f649839457f35"
  ],
  "backlinks": null,
  "word_count": 299,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`SyncCommand` triggers an on-demand sync for a specific database managed by a running litestream daemon. The normal sync interval in litestream is configurable (defaulting to 1 second), but there are scenarios where immediate replication is needed — for example, just before taking a database offline for maintenance, or as part of a deployment script that wants to confirm backup currency before proceeding.

## Wait Mode

The `-wait` flag is the key behavioral difference from a fire-and-forget sync trigger. Without `-wait`, the daemon starts the sync but the CLI returns immediately. With `-wait`, the HTTP response is not sent until the sync has completed all the way to the remote replica (not just local LTX file writing). The usage text documents this distinction: "Block until sync completes including remote replication."

This matters for deployment scripts that use `litestream sync -wait` as a pre-flight check before a blue/green deployment switch: they need confirmation that the replica is fully caught up, not just that a sync was scheduled.

## IPC Pattern

Same pattern as `start.go`, `stop.go`, and `register.go`:

1. Validate arguments (one database path, positive timeout, no extra args)
2. Build Unix socket HTTP client with configured timeout
3. POST `litestream.SyncRequest{Path, Wait, Timeout}` to `http://localhost/sync`
4. Parse `litestream.SyncResponse` and pretty-print as JSON

## Timeout Validation

Unlike `stop.go`, `sync.go` explicitly validates that `-timeout` is greater than 0 before making the network call. A zero timeout would cause the HTTP client to time out immediately on every call. The validation error message `"timeout must be greater than 0"` makes this a user-visible requirement.

## Known Gaps

The timeout is labeled "best-effort" in the usage text — the daemon may not strictly honor the timeout if a sync is in progress. No test for the JSON output format of a successful sync response.