---
{
  "title": "Internal Test Suite for DB: Metrics, Edge Cases, and Regression Tests",
  "summary": "White-box tests for the `DB` type written in the `litestream` package (not `litestream_test`), giving access to unexported methods. Covers Prometheus metric updates, WAL calculation overflow, WAL page coverage during database growth, checkpoint-snapshot interaction, and specific regression tests for issues #994 and #997.",
  "concepts": [
    "internal test",
    "white-box testing",
    "Prometheus testutil",
    "calcWALSize overflow",
    "WAL offset edge case",
    "checkpoint-snapshot interaction",
    "issue #994",
    "issue #997",
    "releaseReadLock",
    "writeLTXFromWAL",
    "page coverage",
    "L0 retention metrics",
    "testReplicaClient mock",
    "feedback loop"
  ],
  "categories": [
    "testing",
    "litestream",
    "sqlite",
    "metrics",
    "test"
  ],
  "source_docs": [
    "d0d4eeea1935165e"
  ],
  "backlinks": null,
  "word_count": 496,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

Being in package `litestream` (not `litestream_test`), this file can test unexported methods like `verify()`, `releaseReadLock()`, `writeLTXFromWAL()`, and `calcWALSize()`. It uses a minimal `testReplicaClient` mock to avoid import cycles.

## Overflow Regression: TestCalcWALSize

Regression test for a uint64 overflow when computing WAL size with large page sizes. SQLite supports page sizes up to 65536 bytes; multiplying page count by page size in 32-bit arithmetic could silently overflow, producing a WAL size that appears smaller than the actual file. The fix uses 64-bit arithmetic throughout.

## Metric Tests

Several tests verify that Prometheus metrics are updated correctly:

- `TestDB_Sync_UpdatesMetrics` — after sync, `db_size_bytes`, `wal_size_bytes`, and `total_wal_bytes` must reflect the current state
- `TestDB_Checkpoint_UpdatesMetrics` — after checkpoint, the appropriate checkpoint counter is incremented
- `TestDB_ReplicaSync_OperationMetrics` — replica PUT count and bytes are tracked when LTX files are uploaded
- `TestDB_Sync_ErrorMetrics` — `sync_error_total` increments when sync fails
- `TestDB_Checkpoint_ErrorMetrics` — `checkpoint_error_total` increments when checkpoint fails
- `TestDB_L0RetentionMetrics` — L0 retention gauges are set during enforcement

All metric tests use `prometheus/testutil` for assertion rather than reading the HTTP endpoint, which is faster and more precise.

## WAL Offset Edge Cases

`TestDB_Verify_WALOffsetAtHeader` and `TestDB_Verify_WALOffsetAtHeader_SaltMismatch` test the case where a stored LTX file's WAL offset points exactly to the WAL file header (offset 0). This edge case arises when a checkpoint has just occurred and the WAL is at position 0. The `_SaltMismatch` variant confirms that a different WAL salt triggers a snapshot rather than an incremental sync, preventing the database from diverging from its replica.

## Read Lock Double-Rollback: TestDB_releaseReadLock_DoubleRollback

Verifies that calling `releaseReadLock()` after the read transaction has already been rolled back (e.g., by a database error) does not panic or return an error. This defensive pattern prevents a crash during error recovery paths where the transaction state is uncertain.

## Checkpoint-Snapshot Interaction

`TestDB_CheckpointDoesNotTriggerSnapshot` and `TestDB_MultipleCheckpointsWithWrites` guard against a feedback loop identified in issue #997: bulk inserts followed by a checkpoint could trigger an unnecessary full snapshot, which in turn would trigger more checkpoints, creating a runaway cycle of disk writes. The tests confirm that a checkpoint followed immediately by a sync does not produce a snapshot unless the WAL salt has actually changed.

## Page Coverage During Growth

`TestDB_WALPageCoverage_AllNewPagesPresent` and `TestDB_WriteLTXFromWAL_PageGrowthCoverage` verify that when SQLite grows the database (adds new pages), all new pages are included in the LTX file even if they appear after the last previously-seen page. A naive page-map implementation might miss pages beyond the previous maximum page number.

## Issue Regression Tests

- **Issue #994** (`TestDB_Issue994_RunawayDiskUsage`) — reproduces a scenario where the local litestream meta directory grew unboundedly because L0 retention was not being enforced after compaction. Verifies that the directory size stays bounded after many syncs.
- **Issue #997** (`TestDB_IdleCheckpointSnapshotLoop`) — reproduces the idle checkpoint feedback loop where a checkpoint during idle periods triggered unnecessary snapshots.

## Known Gaps

The `testReplicaClient` mock does not implement all `ReplicaClient` methods (e.g., `DeleteAll`). Tests that need full client behavior use the file replica client from `litestream/file`.