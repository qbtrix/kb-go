---
{
  "title": "External Test Suite for DB: Integration and Behavioral Tests",
  "summary": "Integration tests for the `DB` type covering sync, compaction, snapshot, retention, vacuum handling, idle sync behavior, checkpoint timing, and concurrent map access safety. Uses real SQLite databases and file-based replica clients to exercise the full replication pipeline.",
  "concepts": [
    "DB",
    "Sync",
    "Compact",
    "Snapshot",
    "EnforceRetention",
    "VACUUM handling",
    "idle sync",
    "checkpoint timing",
    "ConcurrentMapWrite",
    "SyncStatus",
    "SyncAndWait",
    "ResetLocalState",
    "page count decrease",
    "CRC64",
    "testingutil.MustOpenDBs"
  ],
  "categories": [
    "testing",
    "litestream",
    "sqlite",
    "replication",
    "test"
  ],
  "source_docs": [
    "5c382f2603f86797"
  ],
  "backlinks": null,
  "word_count": 513,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

In package `litestream_test`, this file tests `DB` through its public API. Tests create real SQLite databases, perform actual SQL operations, and verify that the resulting LTX files, snapshots, and retention states match expectations.

## Path and Meta Path Tests

`TestDB_Path`, `TestDB_WALPath`, and `TestDB_MetaPath` verify the path derivation conventions:
- WAL path is `<db>-wal`
- Default meta path is `.<db>-litestream` in the same directory
- Custom meta paths are honored when set explicitly

## Sync and WAL Tests

`TestDB_Sync` exercises the full sync pipeline: open database, write data, call `Sync()`, and verify that LTX files appear in the local meta directory. Includes tests for:
- Basic WAL sync after a write
- Sync after VACUUM (which shrinks the database and resets WAL state)
- Idle sync (no writes) does not create new LTX files — a critical check because idle syncs that produce files would cause unbounded storage growth
- Sync after `VACUUM` correctly handles the page count decrease

`TestDB_SyncAfterVacuum` specifically addresses the case where SQLite shrinks the database (reduces page count). The WAL after a VACUUM contains only the pages that survived, not a full database snapshot. If litestream doesn't handle the page count decrease, it may try to read pages that no longer exist.

`TestDB_NoLTXFilesOnIdleSync` verifies that polling syncs on a quiescent database do not generate empty LTX files. Without this guard, a database that is never written would accumulate thousands of zero-byte LTX files over time.

## Checkpoint Timing

`TestDB_DelayedCheckpointAfterWrite` verifies that writes that happen before the configured `CheckpointInterval` elapses do not trigger premature checkpoints. Premature checkpoints could cause SQLite's WAL to be reset before litestream has replicated all pages, leading to data loss on the replica.

## Compaction and Snapshot

`TestDB_Compact` exercises multi-level compaction by writing data, syncing, and then compacting to each level. `TestDB_Snapshot` verifies that `Snapshot()` produces a valid full-database LTX file at level 9. `TestCompaction_PreservesLastTimestamp` confirms that the compacted file retains the timestamp of the most recent source file, which is important for time-based retention calculations.

## Retention

`TestDB_EnforceRetention` verifies that snapshot and L0 files older than the retention window are deleted. `TestDB_EnforceRetentionByTXID_LocalCleanup` confirms that local LTX files are cleaned up when retention is enforced by TXID. The `_RetentionDisabled` variants confirm that setting `RetentionEnabled = false` skips remote deletion.

## Concurrent Safety

`TestDB_ConcurrentMapWrite` specifically exercises the `maxLTXFileInfos` map, which stores cached file info per compaction level. The test launches concurrent sync goroutines to detect races, addressing a specific bug where map writes were not protected by the appropriate mutex.

## SyncStatus and SyncAndWait

`TestDB_SyncStatus` confirms the status reporting reflects actual replica state. `TestDB_SyncAndWait` verifies that the blocking sync returns after the replica acknowledges receipt.

## ResetLocalState

`TestDB_ResetLocalState` verifies that after calling `ResetLocalState()`, the LTX directory is gone. The next sync creates a new snapshot rather than attempting to resume from the deleted state.

## Known Gaps

No test for `EnsureExists()` in scenarios where the parent directory does not exist. CRC64 checksum test (`TestDB_CRC64`) verifies the checksum is non-zero but does not test against a known value, so a checksum algorithm change would not be caught.