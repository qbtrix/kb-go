---
{
  "title": "Litestream Integration Scenario Tests: Fresh Start, Integrity, and Deletion",
  "summary": "Three end-to-end integration tests that cover Litestream's core lifecycle scenarios: starting replication against a database that does not yet exist, verifying data integrity through a replicate-and-restore cycle, and ensuring the replica survives deletion of the source database. Together they form a smoke-test suite for the most common operational patterns.",
  "concepts": [
    "fresh start",
    "database integrity",
    "database deletion",
    "replication lifecycle",
    "WAL checkpoint",
    "LTX files",
    "restore",
    "integrity_check",
    "scenario testing",
    "litestream",
    "SQLite"
  ],
  "categories": [
    "integration-testing",
    "litestream",
    "data-integrity",
    "resilience",
    "test"
  ],
  "source_docs": [
    "c8a7f4c6bc28797c"
  ],
  "backlinks": null,
  "word_count": 491,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

These three tests verify that Litestream behaves correctly across the scenarios operators are most likely to encounter in production:

1. **`TestFreshStart`** — Litestream is launched before the database file exists.
2. **`TestDatabaseIntegrity`** — A complex schema survives replication without data loss or corruption.
3. **`TestDatabaseDeletion`** — Deleting the source database mid-replication does not destroy the replica.

## TestFreshStart

The motivation for this test is a common deployment pattern: a container starts Litestream at boot before the application has written its first row. Without explicit handling, Litestream could miss the database creation event and never begin replication.

The test sequence is:

1. Start Litestream pointing at a non-existent path.
2. Wait 2 seconds, then create the database and insert data.
3. Wait 3 seconds, then insert 100 more rows.
4. Stop Litestream, restore to a new path, and verify row counts match.

The log inspection step (`db.GetLitestreamLog()`) captures whether Litestream detected the newly created file — this is the acceptance criterion for the "late database creation" code path.

## TestDatabaseIntegrity

This test guards against subtle replication bugs that would not be caught by simple row counts. It creates a relational schema with three tables (users, posts, comments), populates them with foreign-key relationships, and then runs `PRAGMA integrity_check` on both the source and restored databases.

The intent is to expose corruption that could arise from:

- WAL checkpointing racing with snapshot creation.
- Partial page application during restore.
- LZ4 decompression errors leaving a valid-looking but internally inconsistent B-tree.

The 10-second sleep after starting Litestream is sized to give the replication engine time to produce at least one snapshot before the process is stopped.

## TestDatabaseDeletion

This test addresses a real-world failure mode: a misconfigured deployment script deletes the SQLite file while Litestream is running. The test verifies:

- Litestream logs errors (expected) but does not crash.
- The replica directory still contains files after the source is gone.
- A subsequent restore from that replica returns the correct 100-row dataset.

The file count comparison (`fileCount` before vs `finalFileCount` after) notes that compaction may reduce the count — the test accepts any non-zero count rather than requiring exact equality, which prevents false failures from compaction running during the deletion window.

## Removed Tests

A comment near the bottom of the file documents that `TestReplicaFailover` was removed because Litestream v5 no longer supports multiple replicas on a single database. This is documented inline rather than deleted silently, which helps contributors understand why the feature is absent.

## Known Gaps

- All timing is sleep-based (`time.Sleep`). There is no polling or retry logic to wait for replication events, making these tests sensitive to CPU load in CI.
- `TestFreshStart` checks for errors in the Litestream log but does not fail the test if errors are present — it only logs them. A future improvement would assert a maximum error count.
- `TestDatabaseDeletion` does not verify which specific errors Litestream emits, only their count.