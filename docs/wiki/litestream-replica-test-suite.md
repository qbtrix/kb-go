---
{
  "title": "Litestream Replica Test Suite",
  "summary": "Comprehensive tests for the Replica type covering sync, restore, time-bounded recovery, and forward-follow mode. Includes backward compatibility helpers for reading v0.3.x backup formats and regression tests for specific issue scenarios.",
  "concepts": [
    "Replica",
    "LTX",
    "WAL",
    "sync",
    "point-in-time restore",
    "follow mode",
    "v3 format",
    "LZ4",
    "TXID",
    "backup compatibility",
    "shadow WAL",
    "checkpoint",
    "data loss recovery",
    "issue 781"
  ],
  "categories": [
    "testing",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "f17db8632a020fd9"
  ],
  "backlinks": null,
  "word_count": 529,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test suite validates the full lifecycle of a Litestream `Replica`: initial synchronization, point-in-time restore, format migration from legacy v0.3.x backups, and the "follow" mode where a replica tail-reads WAL data for live recovery.

## Core Sync Tests

`TestReplica_Sync` exercises the primary replication flow. It opens a database, runs an initial sync to establish a baseline, checkpoints and truncates the WAL to a known state, executes a DDL statement to produce new WAL content, then syncs again and verifies the resulting LTX file via the decoder's `Verify()` path. The explicit truncation before writing ensures the sync captures exactly one segment — without it, earlier WAL frames would pollute the result and make assertions ambiguous.

## Data Loss Recovery Regression

`TestReplica_RestoreAndReplicateAfterDataLoss` reproduces the scenario documented in issue #781. When a database is restored to an earlier state (lower TXID) but a replica already holds a higher TXID, subsequent writes silently fail to replicate because the replica believes it is ahead. The fix compares database position against replica position in `DB.init()` and fetches the latest L0 file from the replica to trigger a corrective snapshot on the next sync cycle. The test drives the full four-step reproduction: create, replicate, restore-to-old, insert, replicate again, restore final, verify.

## Restore Planning and Time Bounds

`TestReplica_CalcRestorePlan` and `TestReplica_TimeBounds` verify that the restore planner selects the correct LTX files for a given TXID target and that time-based windowing correctly excludes files outside the requested range. These tests exist because miscalculation in either dimension silently produces an incomplete or incorrect restore.

## Format Compatibility: v0.3.x

The helper types `v3SnapshotData` and `v3WALSegmentData`, along with `createV3Backup`, `writeV3Snapshot`, and `writeV3WALSegment`, reconstruct the old LZ4-compressed backup layout. `TestReplica_RestoreV3` and `TestReplica_Restore_BothFormats` confirm the current code can read backups created by older Litestream versions. Without these, a format-breaking refactor could silently corrupt recovery for users who have months of old backup data.

## Follow Mode

The follow-mode tests (`TestReplica_Restore_Follow_*`) validate a streaming restore that continuously applies new WAL segments as they arrive rather than stopping at a fixed TXID. Edge cases covered include:

- **Incompatible flags**: follow cannot be combined with certain restore options — the test ensures a clear error rather than silent misbehavior.
- **Context cancellation**: the follow loop must exit cleanly when cancelled, not deadlock.
- **TXID file tracking**: the replica writes a TXID marker file so the consumer knows the last applied transaction. Tests verify write, read, and staleness detection.
- **Crash recovery**: after an unclean shutdown the marker file may be stale. The test ensures the replica re-derives the correct position from the backup rather than trusting the potentially-stale file.
- **No TXID file**: when the marker is absent the replica should start from the beginning, not error.

## Utility Helpers

`createTestSQLiteDB` produces a minimal but structurally valid SQLite file. `createTestWALData` generates a matching WAL header. Both exist because restore logic validates database headers before applying WAL frames; malformed inputs would cause misleading test failures unrelated to the code under test.

## Known Gaps

- `TestReplica_Sync` ends with a `TODO(ltx): Restore snapshot and verify` comment. The test confirms that replication writes data but does not yet restore and re-verify it end-to-end from the written files.