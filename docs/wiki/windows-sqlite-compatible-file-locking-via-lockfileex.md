---
{
  "title": "Windows SQLite-Compatible File Locking via LockFileEx",
  "summary": "Implements SQLite-compatible exclusive file locking on Windows using the Windows API LockFileEx with byte-range parameters that match SQLite's pending-byte and shared-region constants.",
  "concepts": [
    "LockFileEx",
    "UnlockFileEx",
    "OVERLAPPED",
    "Windows file locking",
    "SQLite locking protocol",
    "pending byte",
    "shared region",
    "LOCKFILE_EXCLUSIVE_LOCK",
    "cross-platform",
    "rollback"
  ],
  "categories": [
    "platform",
    "concurrency",
    "sqlite",
    "litestream"
  ],
  "source_docs": [
    "9f6355e331fa9c78"
  ],
  "backlinks": null,
  "word_count": 251,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This is the Windows counterpart to `lock_unix.go`. It provides the same `LockFileExclusive` and `UnlockFile` API but uses Windows' `LockFileEx` / `UnlockFileEx` rather than `fcntl`. The byte-range constants (`sqlitePendingByte`, `sqliteSharedFirst`, `sqliteSharedSize`) are identical to the Unix version, ensuring litestream's locking is compatible with SQLite's expectations on all platforms.

## Windows Overlapped Locking

Windows byte-range locks are specified using `OVERLAPPED` structures that encode the byte offset. The `windows.Overlapped` struct's `Offset` field carries the start byte, and `LockFileEx` takes a length separately. This is more verbose than Unix `flock_t` but achieves the same semantic: lock a specific contiguous byte range within the file.

`LOCKFILE_EXCLUSIVE_LOCK` is passed as the flags parameter to request a write (exclusive) lock rather than a shared (read) lock.

## Two-Phase Acquisition with Rollback

As on Unix, `LockFileExclusive` locks the pending byte first, then the shared region. If locking the shared region fails, the pending byte lock is released via `UnlockFileEx` before returning the error. This rollback prevents orphaned locks that would block SQLite readers indefinitely.

## Unlock

`UnlockFile` releases the shared region first, then the pending byte. Both are always attempted. The Windows equivalent of `errno` propagation means the first non-nil error from `UnlockFileEx` is returned.

## Known Gaps

The `windows.Handle` cast from `f.Fd()` is a standard pattern, but `f.Fd()` on Windows returns a `uintptr` that may be invalidated if the garbage collector moves `f` while the `uintptr` is live. Go's `runtime.KeepAlive` pattern is not used here, though in practice file descriptors are not moved by the GC.