---
{
  "title": "Litestream VFS: SQLite Virtual File System for Read Replicas and Write Forwarding",
  "summary": "Implements a custom SQLite Virtual File System (VFS) that intercepts all SQLite I/O, routes reads through a remote replica client backed by LTX-compacted storage, and optionally forwards writes back to the replica store. It also manages compaction, background hydration to a local file, and per-connection time-travel queries.",
  "concepts": [
    "SQLite VFS",
    "virtual file system",
    "LTX files",
    "compaction",
    "hydration",
    "time-travel queries",
    "write forwarding",
    "read replica",
    "page cache",
    "LRU cache",
    "TXID",
    "conflict detection",
    "ErrConflict",
    "litestream"
  ],
  "categories": [
    "litestream",
    "storage",
    "sqlite",
    "architecture"
  ],
  "source_docs": [
    "d4742c7d8350a6f5"
  ],
  "backlinks": null,
  "word_count": 649,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`vfs.go` is the largest and most complex file in Litestream. It implements `sqlite3vfs.VFS` and `sqlite3vfs.File` — the two interfaces the `psanford/sqlite3vfs` package needs to register a custom storage backend with the SQLite C library. Every SQLite `open`, `read`, `write`, `lock`, and `sync` call flows through this code.

## VFS Struct

`VFS` is the factory. Its fields configure the behavior of every `VFSFile` it opens:

- `PollInterval` / `CacheSize` — how often to check for new LTX files and how many pages to cache in an LRU.
- `WriteEnabled` / `WriteSyncInterval` / `WriteBufferPath` — controls for the optional write-forwarding path.
- `HydrationEnabled` / `HydrationPath` — whether to maintain a local copy of the database for fast reads.
- `CompactionEnabled` / `CompactionLevels` — whether to run the four-level compactor.

The `VFS.Open` method distinguishes between the main database file and temp/transient files SQLite creates internally. Main DB files get a full `VFSFile`; temp files get a `localTempFile` backed by the OS filesystem. This split is necessary because SQLite uses temp files for sort buffers and sub-queries — routing those through the remote replica would be both slow and semantically wrong.

## VFSFile: Read Path

`ReadAt` on a `VFSFile` serves pages from a multi-level cache and falls back to the replica client. The `pollReplicaClient` goroutine runs continuously, fetching new LTX files from the remote store and applying them to the in-memory page index. `pollLevel` handles one compaction level at a time, building a page map from LTX segments.

Time-travel reads use `SetTargetTime` / `ResetTime` to rebuild the page index up to a specific timestamp. `rebuildIndex` walks the LTX file list and stops at the first file whose `CreatedAt` exceeds the target, then applies pages only from files before that point.

## VFSFile: Write Path

When `WriteEnabled` is true, `WriteAt` buffers dirty pages into a local write buffer file. `Sync` calls `syncToRemote`, which assembles an LTX file from the dirty pages using `createLTXFromDirty` and uploads it to the replica client. A `syncLoop` goroutine runs at `WriteSyncInterval` to flush even if the application does not call `Sync` explicitly.

`checkForConflict` detects concurrent writers by comparing the expected TXID against the latest remote TXID before committing. If another writer has advanced the TXID, `ErrConflict` is returned and the transaction must be retried. This prevents split-brain scenarios where two VFS instances both believe they hold the latest state.

## Hydration

`Hydrator` maintains a local file copy of the database by replaying LTX files. Once hydration is complete (all pages applied up to the latest TXID), reads can be served from the local file instead of the remote replica, dramatically reducing read latency. `applySyncedPagesToHydratedFile` keeps the hydrated file synchronized as new LTX files arrive.

## Compaction

`startCompactionMonitors` launches one goroutine per compaction level. Each goroutine calls `Compact` at its configured interval (`DefaultCompactionLevels`: L1 every 30s, L2 every 5m, L3 every 1h). Compaction merges multiple L0 LTX files into a single higher-level file, reducing the number of files the poll loop must process on startup.

`monitorSnapshots` periodically creates full snapshots (complete page images) so that restore does not have to replay arbitrarily long LTX chains.

## Connection Registry

The exported functions `RegisterVFSConnection`, `UnregisterVFSConnection`, `SetVFSConnectionTime`, `GetVFSConnectionTXID`, and `GetVFSConnectionLag` maintain a global map from SQLite connection pointers (as `unsafe.Pointer`) to `VFSFile` instances. This enables higher-level Go code to inspect or change the time-travel position of a live connection without exposing the VFS internals.

## Known Gaps

- `parseTimeValue` tries RFC3339 first, then falls back to `go-dateparser` for natural language expressions. The fallback is noted as best-effort and may return unexpected results for ambiguous inputs like `"tomorrow"`.
- `isRetryablePageError` classifies errors for retry but the classification logic is not exhaustive — new error types from the replica client may silently be treated as non-retryable.
- `ensureTempDir` uses `sync.Once` to create the temp directory. If creation fails, subsequent temp file opens return a cached error and cannot recover without restarting the VFS.