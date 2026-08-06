---
{
  "title": "Test Suite: VFS Write and Sync Integration Tests",
  "summary": "vfs_write_integration_test.go covers the full spectrum of writable VFS behavior: basic write-sync-read cycles, conflict detection, rollback semantics, concurrent readers, buffer management, and restore consistency. It is the authoritative integration suite for VFS write correctness.",
  "concepts": [
    "VFS write buffer",
    "conflict detection",
    "rollback semantics",
    "sync interval",
    "dirty pages",
    "manual sync",
    "write-read cycle",
    "concurrent readers",
    "buffer corruption",
    "LTX sync"
  ],
  "categories": [
    "testing",
    "litestream",
    "VFS",
    "integration",
    "test"
  ],
  "source_docs": [
    "d2a60e3180918ee3"
  ],
  "backlinks": null,
  "word_count": 391,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The VFS layer in read-only mode is relatively straightforward. Write support is significantly more complex: the VFS must buffer dirty pages locally, detect conflicts when the remote replica has advanced, and correctly handle rollbacks without leaving the local buffer in an inconsistent state. This test file covers all of those scenarios.

## Write Buffer Lifecycle

Several tests focus on the write buffer:

- **`TestVFS_WriteBufferDiscardedOnOpen`**: verifies that any leftover buffer from a previous session is discarded when the VFS is opened. This prevents a crashed session's dirty pages from being committed in the next session.
- **`TestVFS_WriteBufferDuplicatePages`**: verifies that writing the same page twice within one transaction correctly keeps only the latest version. SQLite may write a page multiple times within a transaction (e.g., page splits during a large insert).
- **`TestVFS_ExistingBufferDiscarded`**: verifies that a buffer file left on disk from a previous crash is removed on open, even if it contains valid-looking data.
- **`TestVFS_WriteBufferCorrupted`**: injects a corrupted buffer file and verifies the VFS detects and discards it rather than committing garbage pages.

## Conflict Detection

`TestVFS_ConflictDetection` writes data via the VFS, then advances the remote replica (simulating another writer), then tries to sync. The VFS must detect that the remote has moved ahead of the local buffer's base transaction ID and reject the sync with a conflict error. `TestVFS_NoConflictWhenRemoteUnchanged` verifies the inverse — no false conflicts when nothing has changed.

## Rollback Semantics

`TestVFS_RollbackRestoresOriginalState`, `_RollbackAfterUpdate`, and `_RollbackAfterDelete` verify that rolling back a transaction leaves the VFS in exactly the state it was before the transaction began. This is critical for applications that use `BEGIN/ROLLBACK` for optimistic concurrency — a page that leaked from a rolled-back transaction into the buffer would cause silent data corruption.

## Sync Modes

- `TestVFS_PeriodicSync`: verifies automatic sync fires within the configured `SyncInterval`.
- `TestVFS_SyncDuringTransaction`: verifies sync is deferred while a write transaction is open.
- `TestVFS_ManualSyncOnly` (`SyncInterval=0`): verifies no automatic sync occurs, requiring explicit `Sync()` calls.

## Helper Factories

`newWritableVFS` and `newReadOnlyVFS` create VFS instances with different configurations. This separation makes test intent clear and prevents read-only tests from accidentally testing write behavior.

## Known Gaps

- `TestVFS_LargeTransaction` is listed but the exact page count threshold that defines "large" is not documented.
- Tests use `require` from `testify` for assertions, mixing assertion styles with the raw `t.Fatal` pattern used elsewhere in the file.