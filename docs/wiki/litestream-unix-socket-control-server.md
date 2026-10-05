---
{
  "title": "Litestream Unix Socket Control Server",
  "summary": "Implements an HTTP server bound to a Unix domain socket that exposes runtime control endpoints for managing databases, triggering syncs, querying transaction IDs, and registering or unregistering replicated databases. This serves as the IPC channel between the Litestream daemon and CLI commands.",
  "concepts": [
    "Unix socket",
    "HTTP server",
    "control plane",
    "IPC",
    "sync endpoint",
    "TXID",
    "register",
    "unregister",
    "start",
    "stop",
    "pprof",
    "SocketConfig",
    "PathExpander",
    "Store",
    "daemon"
  ],
  "categories": [
    "server",
    "control plane",
    "litestream"
  ],
  "source_docs": [
    "1cd816b0c9af551f"
  ],
  "backlinks": null,
  "word_count": 453,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`Server` binds a standard `net/http.Server` to a Unix socket instead of a TCP port. Unix sockets are used rather than TCP because they are file-system-based, support filesystem permissions for access control, and avoid port conflicts. The `SocketConfig` struct wraps path and permission settings so callers can configure the socket without importing the server type.

## Endpoints

### `/start` and `/stop`

`handleStart` and `handleStop` accept a database file path and optional timeout, then call `store.EnableDB` / `store.DisableDB`. A `Timeout` field in the request body allows the caller to block until the operation completes or give up after a deadline — useful in scripted workflows where the caller needs to know the database is actively replicating before proceeding.

### `/txid`

`handleTXID` returns the current transaction ID for a given database path, allowing external tooling to determine the latest replicated transaction without reading the SQLite file directly.

### `/sync`

`handleSync` triggers an on-demand sync for a specific database. The `Wait` field in `SyncRequest` causes the handler to block until the sync completes rather than returning immediately. `ReplicatedTXID` in the response allows the caller to confirm that a specific transaction has been durably replicated.

### `/list`

`handleList` returns a `ListResponse` containing a `DatabaseSummary` for every database currently tracked by the store.

### `/info`

`handleInfo` returns server metadata including the `Version` field set at startup. This endpoint exists so CLI tools can verify they are speaking to a compatible daemon version.

### `/register` and `/unregister`

`handleRegister` and `handleUnregister` dynamically add and remove databases from the store at runtime without restarting the daemon. This supports use cases where databases are created or deleted while Litestream is running.

## Path Expansion

`expandPath` uses a pluggable `PathExpander` function to resolve relative or symbolic paths before passing them to the store. Without this, paths in requests might not match the absolute paths the store uses internally.

## Lifecycle

`Start` creates the socket, sets permissions from `SocketPerms`, and starts the HTTP server in a goroutine. `Close` cancels the context, closes the listener, and waits for the `wg` wait group. The wait group is necessary because the HTTP server may be in the middle of handling a long-running `/sync?wait=true` request when shutdown is requested.

## pprof Integration

The import of `net/http/pprof` registers profiling endpoints on the server's `ServeMux`. This allows live CPU and heap profiles to be captured from the daemon via the Unix socket during debugging.

## Known Gaps

- The Unix socket uses filesystem permissions (`SocketPerms`) for access control; there is no authentication or TLS. On multi-user systems, anyone with read access to the socket directory can issue control commands.
- `PathExpander` defaults to `nil`, which means paths are used as-is; callers must set this if relative paths need resolving.