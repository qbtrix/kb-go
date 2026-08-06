---
{
  "title": "Litestream VFS Write Mode Test Suite",
  "summary": "Test suite covering VFSFile write-path behavior in Litestream, including buffered writes, sync-to-remote, conflict detection, and dynamic enable/disable of write mode. Uses an in-memory mock replica client to test all write scenarios deterministically without real network or disk I/O.",
  "concepts": [
    "VFSFile",
    "write mode",
    "LTX",
    "conflict detection",
    "SetWriteEnabled",
    "writeTestReplicaClient",
    "buffer file",
    "sync",
    "RESERVED lock",
    "replica client",
    "litestream",
    "sqlite3vfs",
    "ErrConflict"
  ],
  "categories": [
    "testing",
    "sqlite",
    "litestream",
    "storage",
    "test"
  ],
  "source_docs": [
    "c647172fb20cf22d"
  ],
  "backlinks": null,
  "word_count": 594,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file tests the write side of Litestream's VFS layer. By default a Litestream-backed database is read-only, with writes routed through the primary. These tests cover the experimental write-enabled mode where a VFSFile can buffer mutations locally and then sync them as new LTX files to the replica backend.

## Test Infrastructure

**`writeTestReplicaClient`** is the central mock. It stores LTX file data in two in-memory maps: one keyed by `(level, minTXID, maxTXID)` for metadata (`ltxFiles`) and one for raw bytes (`ltxData`). It supports the full `ReplicaClient` interface: listing, opening, writing, and deleting LTX files. All operations are mutex-protected, making it safe for concurrent tests.

**`writeTestFileIterator`** provides a simple in-order `ltx.FileIterator` over a slice of `*ltx.FileInfo`, needed because the real iterator is backed by a remote listing.

**`failingWriteClient`** wraps the write client and returns an error after a configurable number of `WriteLTXFile` calls. This enables testing partial-write failure recovery.

`setupWriteableVFSFile` and `openWriteVFSFile` are helpers that create a fully initialized `VFSFile` with `WriteEnabled = true` and an LTX seed file in the mock client, avoiding boilerplate across dozens of tests.

## Write Buffer Lifecycle

`TestVFSFile_WriteEnabled` confirms that a file opened in write mode creates a local buffer file on disk. `TestVFSFile_WriteBuffer` verifies that `WriteAt` accumulates dirty pages in the buffer, and `TestVFSFile_WriteBufferClearAfterSync` ensures the buffer is flushed empty after a successful `Sync`. `TestVFSFile_WriteBufferDiscardedOnOpen` checks that a stale buffer from a previous session is discarded at open time — this prevents replaying outdated writes if a process crashed after buffering but before syncing.

## Sync and Conflict Detection

`TestVFSFile_SyncToRemote` performs a full write-then-sync cycle and verifies that a new LTX file appears in the mock client. `TestVFSFile_ConflictDetection` simulates an external writer advancing the remote TXID between a local write and a sync call — the sync must return `ErrConflict`. Without conflict detection, two writers could produce branching LTX histories and corrupt the replica. `TestVFS_RealConflict_StillDetected` reproduces the same scenario at the `VFS` level to confirm conflict checks survive the additional layer of indirection.

## SetWriteEnabled Transitions

`SetWriteEnabled` dynamically toggles write mode on a live `VFSFile`. The test suite covers:

- **Enable from cold** — `TestSetWriteEnabled_ColdEnable` confirms enabling on a file that was never write-capable works.
- **Disable syncs dirty pages** — `TestSetWriteEnabled_DisableSyncsDirtyPages` verifies that disabling write mode flushes any unflushed buffer to the remote before the file goes read-only, preventing data loss.
- **Disable waits for in-flight transaction** — `TestSetWriteEnabled_DisableWaitsForTransaction` holds a `RESERVED` lock and confirms `SetWriteEnabled(false)` blocks until the transaction completes. This prevents the disable from racing a commit.
- **Disable with timeout** — `TestSetWriteEnabled_DisableWithTimeout` passes a context with a short deadline and expects an error when the in-flight transaction does not finish in time.
- **No-op idempotency** — `TestSetWriteEnabled_NoOpWhenAlreadyInState` confirms calling `SetWriteEnabled(true)` on an already-enabled file is a no-op without side effects.

## Concurrency

`TestVFS_ConcurrentOpenAllSucceed` opens ten VFSFile connections in parallel and asserts none fail, verifying that the shared VFS lock does not deadlock under concurrent open. `TestVFS_WriteLockBlocksConcurrentWriters` confirms that acquiring a `RESERVED` lock on one connection blocks another from acquiring `RESERVED` — SQLite's single-writer guarantee must hold at the VFS level. `TestVFS_CloseReleasesWriteSlot` confirms that closing a connection holding `RESERVED` releases the write slot so another connection can proceed.

`TestVFS_UniqueBufferPaths` opens two VFSFile instances and asserts their buffer file paths are distinct. Buffer path collisions would cause writes from one connection to overwrite the other's buffer.

## Known Gaps

The `//go:build vfs` and `// +build vfs` tags gate this entire file, meaning it runs only under explicit tag activation. Standard `go test ./...` skips these tests silently. No TODO or FIXME markers are present.