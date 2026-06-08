---
{
  "title": "Litestream VFS File Test Suite",
  "summary": "Comprehensive test suite for Litestream's SQLite Virtual File System (VFS) layer, covering lock state machines, index isolation, hydration lifecycle, and replica polling. Tests use deterministic mock and blocking replica clients to exercise concurrency correctness and edge cases without real remote storage.",
  "concepts": [
    "VFSFile",
    "LTX",
    "SQLite VFS",
    "lock state machine",
    "mockReplicaClient",
    "blockingReplicaClient",
    "hydration",
    "index isolation",
    "polling cancellation",
    "temp file lifecycle",
    "replica client",
    "pending index",
    "WAL",
    "litestream"
  ],
  "categories": [
    "testing",
    "sqlite",
    "litestream",
    "storage",
    "test"
  ],
  "source_docs": [
    "84b2798e7c2b2e21"
  ],
  "backlinks": null,
  "word_count": 626,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file exercises `VFSFile` — Litestream's custom SQLite VFS implementation that intercepts database reads and routes them through a remote LTX replica stream. The tests ensure correct behavior under concurrent access, network delays, and failure scenarios that would otherwise require a real remote backend to reproduce.

## Test Infrastructure

Three mock types drive the tests:

- **`mockReplicaClient`** — A deterministic stub that serves prebuilt LTX fixture files from an in-memory map. Fixtures are created with `buildLTXFixture` (single-page) and `buildLTXFixtureWithPage`/`buildLTXFixtureWithPages` (multi-page). This lets tests construct exact WAL sequences without touching disk or network.
- **`blockingReplicaClient`** — Wraps a mock client and gates the next `LTXFiles` or `OpenLTXFile` call behind a channel, then signals a `blocked` channel once the call is in-flight. Used to test cancellation paths and race conditions where polling must be interrupted cleanly.
- **`countingReplicaClient`** — Wraps a mock and tallies every method call. Used to verify that expensive operations (e.g. re-fetching the index) are not repeated unnecessarily.

## Lock State Machine

`TestVFSFile_LockStateMachine` walks the SQLite lock progression: `SHARED → RESERVED → EXCLUSIVE → NONE`. It confirms that downgrading via `Lock()` (rather than `Unlock()`) is rejected, and that unlocking to `PENDING` is invalid. These checks prevent silent data corruption — if the VFS allowed illegal lock transitions, SQLite's internal consistency guarantees would break silently.

## Index Isolation Under Concurrent Reads

`TestVFSFile_PendingIndexIsolation` verifies that new LTX frames arriving while a shared lock is held are staged in a pending index rather than immediately applied. A read transaction must see a stable snapshot; applying new pages mid-read would return torn data to SQLite. The race variant (`TestVFSFile_PendingIndexRace`) fires a background goroutine that continuously adds fixtures while the main goroutine holds a lock, confirming the two-index design holds under scheduling pressure.

## Memory Bounding

`TestVFSFile_IndexMemoryDoesNotGrowUnbounded` guards against an accumulation bug: as transactions arrive, superseded page entries in the index must be evicted. Without this, a long-running read-only replica would eventually exhaust heap memory.

## Hydration Lifecycle

Hydration is the process of materializing a local copy of the database file by replaying LTX files. The hydration tests cover:

- **Basic completion** — `TestVFSFile_Hydration_Basic` confirms that once all LTX frames are applied the hydrator signals completion and reads are served from the local file.
- **Reads during hydration** — `TestVFSFile_Hydration_ReadsDuringHydration` ensures reads succeed before the local file is ready by falling back to the remote replica client.
- **Early close** — `TestVFSFile_Hydration_CloseEarly` checks that closing a VFSFile mid-hydration does not leak goroutines or leave partial files.
- **Stale meta recovery** — `TestHydrator_Init_StaleMeta` writes a `.meta` file referencing a future TXID, then checks that `Init` detects the inconsistency, resets to TXID 0, and removes the stale file. Without this guard, a crash after meta write but before data sync would cause the hydrator to skip replaying required frames.
- **Persistent resume** — `TestVFSFile_Hydration_PersistentResumeOnReopen` opens a VFSFile with `hydrationPersistent = true`, hydrates it fully, closes, reopens, and confirms the second open skips re-downloading already-applied frames (verifying file modtime is unchanged).

## Polling Cancellation

`TestVFSFileMonitorStopsOnCancel` and `TestVFSFile_PollingCancelsBlockedLTXFiles` use the `blockingReplicaClient` to confirm that cancelling a context while a poll is blocked causes the goroutine to exit cleanly. This prevents goroutine leaks on shutdown.

## Temp File Management

A cluster of tests (`TestVFS_TempFile*`) verifies that ephemeral SQLite journal files created by the VFS are isolated per-directory, have unique names even with the same basename, are deleted on close, and that exhaustion of the temp-file namespace returns a proper error rather than silently reusing names.

## Known Gaps

No explicit TODO or FIXME markers appear in this file. The `//go:build vfs` build tag means these tests only run when the `vfs` tag is explicitly set, which could cause them to be overlooked in standard CI pipelines if the tag is not configured.