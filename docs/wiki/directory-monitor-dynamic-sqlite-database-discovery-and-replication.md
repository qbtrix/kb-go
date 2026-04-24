---
{
  "title": "Directory Monitor: Dynamic SQLite Database Discovery and Replication",
  "summary": "DirectoryMonitor watches a directory tree using fsnotify, automatically adding new SQLite databases to the Litestream store and removing them when deleted. It applies configurable glob patterns to filter which files to manage and debounces rapid filesystem events to avoid redundant work.",
  "concepts": [
    "directory watcher",
    "fsnotify",
    "dynamic replication",
    "debounce",
    "SQLite discovery",
    "glob pattern",
    "recursive watch",
    "WAL file filtering",
    "store management",
    "DirectoryMonitor"
  ],
  "categories": [
    "litestream",
    "filesystem",
    "replication",
    "monitoring"
  ],
  "source_docs": [
    "4ce58f90eab9eeb7"
  ],
  "backlinks": null,
  "word_count": 407,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The standard Litestream config lists databases explicitly. This is impractical for applications that create many ephemeral databases (e.g., per-tenant SQLite files, test databases). `DirectoryMonitor` solves this by treating a directory as the unit of configuration: any SQLite database that appears under the watched path is automatically replicated.

## fsnotify Integration

The monitor registers watches on directory paths using `github.com/fsnotify/fsnotify`. When a `Create` or `Remove` event fires, it is queued rather than processed immediately. This is because a file creation followed by an immediate write generates multiple events. Processing each event immediately would attempt to open a partially-written database.

## Debounce

```go
const debounceInterval = 250 * time.Millisecond
```

Events are queued in `pendingEvents` and processed by `flushPendingEvents` only after 250ms of inactivity. This collapses rapid event bursts (e.g., a tool creating and immediately locking a database) into a single processing pass. The debounce is implemented by tracking `debounceActive` and resetting a timer on each new event.

## Path Filtering

`shouldSkipPath` rejects SQLite auxiliary files by suffix:
- `*-wal` — write-ahead log
- `*-shm` — shared memory
- `*-journal` — rollback journal

Without this filter, every WAL write would trigger a spurious "new database" event, causing the monitor to attempt to add files that are not databases.

`matchesPattern` applies a glob pattern if configured. This allows operators to manage only `*.db` files in a directory that also contains other file types.

## Recursive Watching

When `recursive` is true, `addInitialWatches` scans subdirectories and registers a watch on each one. `fsnotify` watches are per-directory (not recursive by default on most platforms), so the monitor must explicitly add watches for new subdirectories as they are created.

## Dynamic Store Management

`handlePotentialDatabase` calls `store.AddDB` when a file passes the pattern filter and SQLite magic-byte validation. `removeDatabase` calls `store.RemoveDB`. Both operations are guarded by `mu` to prevent concurrent additions/removals from corrupting the `dbs` map.

`removeDatabasesUnder` handles directory removal: when a watched directory is deleted, all databases registered under that path are removed from the store. Without this, the store would hold stale DB references pointing to deleted paths.

## Known Gaps

- The debounce timer is reset on every event, meaning a continuous stream of events (e.g., a process writing to a database file repeatedly) could delay processing indefinitely.
- SQLite magic-byte validation is mentioned in the design but the implementation detail is in `handlePotentialDatabase`, which may check only by file extension rather than reading file bytes on some code paths.