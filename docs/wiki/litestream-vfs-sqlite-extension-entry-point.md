---
{
  "title": "Litestream VFS SQLite Extension Entry Point",
  "summary": "main.go for the `litestream-vfs` package builds a SQLite loadable extension that registers a virtual file system, allowing any SQLite client to query a Litestream replica directly using `litestream://` as the database URI. It also exposes CGo-exported functions for per-connection time travel and lag inspection.",
  "concepts": [
    "SQLite VFS",
    "loadable extension",
    "CGo",
    "time travel",
    "replica client",
    "LTX files",
    "LITESTREAM_REPLICA_URL",
    "c-shared",
    "connection registration",
    "lag reporting",
    "backend registration"
  ],
  "categories": [
    "litestream",
    "SQLite",
    "VFS",
    "CGo"
  ],
  "source_docs": [
    "dae3c70a8f749b09"
  ],
  "backlinks": null,
  "word_count": 452,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The standard Litestream workflow requires a full restore before querying. The VFS extension eliminates that step by presenting the replica as a SQLite VFS — SQLite reads pages from the VFS, and the VFS fetches them from LTX files in the replica. This enables read-only queries against a continuously-updated replica without local disk space proportional to the database size.

## Build Mode

```go
//go:build SQLITE3VFS_LOADABLE_EXT
```

This build tag gates the entire file. Without it, the package builds as a regular Go library. With it (via `-tags SQLITE3VFS_LOADABLE_EXT`), the package compiles to a C-shared archive that SQLite can load with `.load` or `sqlite3_load_extension`.

The `import "C"` at the top is required for CGo even though no C code is called — it signals the Go toolchain that this file participates in the C ABI, enabling the `//export` directives below.

## LitestreamVFSRegister

`LitestreamVFSRegister` is the extension entry point called by SQLite when the extension is loaded. It:

1. Reads `LITESTREAM_REPLICA_URL` to determine which backend to connect to (S3, ABS, GCS, file, etc.).
2. Constructs a `ReplicaClient` via the registered URL factory (all backends are blank-imported to trigger their `init()` registration).
3. Calls `sqlite3vfs.RegisterVFS` to make the `litestream` VFS name available to SQLite.
4. Returns a `*C.char` error string on failure (the SQLite extension API convention) or nil on success.

## CGo-Exported Functions

The remaining exported functions are called by SQLite via pragma hooks or application code:

| Function | Purpose |
|---|---|
| `GoLitestreamRegisterConnection` | Associates a VFS connection with a specific LTX file snapshot |
| `GoLitestreamUnregisterConnection` | Releases connection state on database close |
| `GoLitestreamSetTime` / `ResetTime` | Pin the VFS to a specific timestamp for time-travel queries |
| `GoLitestreamTime` | Read the current VFS timestamp |
| `GoLitestreamTxid` | Read the transaction ID visible to this connection |
| `GoLitestreamLag` | Report how far behind the VFS is from the latest replica state |

These functions pass `dbPtr` (a C `sqlite3*` pointer cast to `unsafe.Pointer`) to identify the connection in a Go-side map. This is the standard pattern for associating Go state with an opaque C handle.

## Backend Registration

All replica backends are blank-imported:
```go
_ "github.com/benbjohnson/litestream/abs"
_ "github.com/benbjohnson/litestream/s3"
// ...
```
Each `init()` registers its URL factory. This means `LITESTREAM_REPLICA_URL=abs://...` works without any code change — the factory lookup handles dispatch.

## Known Gaps

- `main()` is empty. This is required for the Go toolchain to compile the package as a `c-archive`, but it means the binary cannot be run directly as a command.
- Error handling in `LitestreamVFSRegister` returns a C string, but SQLite may not display that string to users depending on how the extension was loaded, making errors hard to diagnose.