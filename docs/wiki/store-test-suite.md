---
{
  "title": "Store Test Suite",
  "summary": "Tests the Store's compaction, integration, validation, retention, and snapshot-interval behavior. Includes multi-database tests that verify L1 and L2 compaction across independent database instances.",
  "concepts": [
    "Store",
    "compaction",
    "L1",
    "L2",
    "snapshot interval",
    "ValidationMonitor",
    "RetentionEnabled",
    "L0 retention",
    "file replica",
    "integration test",
    "MustOpenDBs",
    "testingutil"
  ],
  "categories": [
    "testing",
    "compaction",
    "litestream",
    "test"
  ],
  "source_docs": [
    "2a1f04700754d53b"
  ],
  "backlinks": null,
  "word_count": 362,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test suite drives `Store` with real databases and a file-based replica client to verify end-to-end behaviors that cannot be tested at the `DB` level alone.

## Compaction Tests

`TestStore_CompactDB` has sub-tests for L1 and L2 compaction. Each sub-test opens two independent databases, registers them with a store configured with tiered compaction intervals, writes data, and asserts that LTX files from lower levels have been merged into higher-level files. The test uses two databases per sub-test to confirm the store compacts each independently and does not merge files across database boundaries.

## Integration Test

`TestStore_Integration` runs a full cycle: write data, sync, compact multiple levels, snapshot, validate. It is the broadest test in the file and serves as a regression gate for any change that affects the multi-step pipeline.

## Snapshot Interval Default

`TestStore_SnapshotInterval_Default` verifies that when `SnapshotInterval` is not set explicitly, the store uses a sensible non-zero default. The test exists because a zero interval would cause the store to snapshot every compaction cycle, generating enormous storage overhead. The guard was added after a regression where the default was accidentally set to zero.

## Validation Monitor

`TestStore_ValidationMonitor` enables `ValidationInterval` and asserts the store performs a restore-and-compare cycle within the expected time window. This is tested separately from `TestStore_Validate` because the monitor uses a background goroutine whose timing is non-deterministic.

`TestStore_Validate` calls the validation logic directly (bypassing the background timer) to assert correctness independently of scheduling.

## Retention

`TestStore_SetRetentionEnabled` toggles `RetentionEnabled` and confirms that when disabled, old L0 files accumulate rather than being pruned. When enabled, files older than `L0Retention` disappear. This test prevents a silent regression where the retention goroutine runs but uses the wrong comparison operator, retaining files that should be deleted.

## Test Infrastructure

All tests use `testingutil.MustOpenDBs` to create database + SQL connection pairs and `file.NewReplicaClient` for local-disk replicas. Compaction intervals are set to very short values (milliseconds to seconds) so tests complete in reasonable wall-clock time without sleeping.

## Known Gaps

- No test covers the `ShutdownSyncTimeout` path — the scenario where `Close` times out waiting for in-flight syncs.
- `TestStore_CompactDB` does not verify that compaction is idempotent when called twice with no new data.