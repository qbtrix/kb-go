---
{
  "title": "Replicate Command: Continuous SQLite Replication Daemon",
  "summary": "Implements the `litestream replicate` sub-command, the long-running daemon that continuously replicates one or more SQLite databases to configured storage backends. It supports subprocess execution, directory monitoring for dynamic database discovery, Prometheus metrics, pprof profiling, a Unix socket IPC server, and an optional MCP HTTP server.",
  "concepts": [
    "ReplicateCommand",
    "litestream daemon",
    "Unix socket IPC",
    "Prometheus metrics",
    "pprof",
    "MCP server",
    "DirectoryMonitor",
    "exec subprocess",
    "one-shot replication",
    "force snapshot",
    "enforce retention",
    "litestream.Store",
    "restoreIfNeeded",
    "go-shellwords",
    "flag positioning"
  ],
  "categories": [
    "cli",
    "litestream",
    "replication",
    "daemon"
  ],
  "source_docs": [
    "3208c2d1f3a2ef98"
  ],
  "backlinks": null,
  "word_count": 506,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`ReplicateCommand` is the heart of litestream's operational mode. When invoked, it loads configuration, initializes all database and replica objects, starts the IPC control server, and then blocks until a signal or sub-command terminates it. It also handles the `--exec` mode where a child process is managed alongside replication.

## Flag Parsing and Configuration Loading

`ParseFlags()` handles two invocation styles:

1. **Config file mode** (no positional args) — reads from the default or specified YAML config file
2. **CLI mode** (db path + replica URLs as positional args) — constructs an in-memory `Config` from the arguments

A guard rejects flags placed after positional arguments (issue #245 reference in tests). This is necessary because Go's `flag.Parse()` stops at the first non-flag argument, so `-exec echo` after `db.sql s3://...` would silently ignore the flag. The guard makes this a hard error with a clear message.

Three "once" flags modify the daemon's behavior:
- `-once` — replicate once and exit rather than running continuously
- `-force-snapshot` — force a snapshot to all replicas on this run (requires `-once`)
- `-enforce-retention` — clean up old snapshots on this run (requires `-once`)

Mutual-exclusion rules: `-force-snapshot` and `-enforce-retention` require `-once`; `-once` and `-exec` are mutually exclusive because `-exec` implies continuous operation.

## Run Loop

`Run()` sets up the full daemon:

- Initializes structured logging via `initLog()`
- Constructs `litestream.Store` with all configured databases
- Starts the Unix socket IPC server (`litestream.Server`) for `register`, `unregister`, `sync`, `start`, and `stop` commands
- Starts the Prometheus metrics endpoint if `Addr` is configured
- Starts pprof on the same HTTP mux when the metrics server is running
- Starts the optional MCP server
- Starts directory monitors for any directory-based DB configs
- Calls `runOnce()` for the initial sync, then enters the main event loop

`runOnce()` handles the one-shot replication path: if `-force-snapshot` is set, it takes a snapshot before the regular sync; if `-enforce-retention` is set, it enforces retention after sync.

## Subprocess Execution (-exec)

When `-exec` is specified, the command parses the exec string with `go-shellwords` (supporting quoted arguments), starts the child process, and then waits for either the child to exit or a signal to arrive. If the child exits with a non-zero code, replication stops and the exit code is propagated. This mode is used to replicate for the lifetime of an application without a separate service manager.

`restoreIfNeeded()` runs before the sub-command starts: if the database does not exist and `RestoreIfDBNotExists` is set, it restores from the newest available replica backup. This enables a pattern where a database is restored automatically on the first run after a deployment to a fresh instance.

## Shutdown

`Close()` stops directory monitors, closes the store (which flushes pending WAL data), shuts down the MCP server, and stops the IPC server. The `done` channel signals the shutdown sync retry loop to stop retrying once the operator has forcefully interrupted.

## Known Gaps

No explicit TODO markers. The `SetDone()` method suggests the shutdown retry loop has some coupling to external interrupt handling that could be cleaner.