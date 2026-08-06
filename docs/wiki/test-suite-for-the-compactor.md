---
{
  "title": "Test Suite for the Compactor",
  "summary": "Comprehensive tests for `Compactor` covering LTX file merging across levels, caching behavior, all three retention enforcement methods, and post-compaction verification. Uses a local file replica client to make tests self-contained without requiring external storage.",
  "concepts": [
    "TestCompactor_Compact",
    "ErrNoCompaction",
    "TXID watermark",
    "CacheGetter",
    "CacheSetter",
    "EnforceRetentionByTXID",
    "EnforceL0Retention",
    "EnforceSnapshotRetention",
    "RetentionEnabled",
    "VerifyLevelConsistency",
    "createTestLTXFile",
    "file.ReplicaClient",
    "compaction levels"
  ],
  "categories": [
    "testing",
    "litestream",
    "compaction",
    "test"
  ],
  "source_docs": [
    "8a3bde3525db95f0"
  ],
  "backlinks": null,
  "word_count": 432,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file exercises every public method of `Compactor` using `file.NewReplicaClient` pointed at a temporary directory. Helper functions create minimal valid LTX files, allowing tests to set up precise TXID ranges and timestamps without touching a real SQLite database.

## Compaction Tests

`TestCompactor_Compact` covers three scenarios:

- **L0ToL1** — creates two L0 files (TXIDs 1 and 2), compacts to L1, and verifies the resulting file spans 1–2
- **NoFiles** — compacting when no source files exist returns `ErrNoCompaction`
- **L1ToL2** — creates L0 files, compacts to L1 twice (with a new L0 file in between), then compacts L1 to L2. Verifies that the L1→L2 compaction picks up all TXIDs 1–3, not just those from the most recent L1 compaction

The L1→L2 test is the most important: it validates the incremental compaction watermark logic. Each compaction starts from the last `MaxTXID` at the destination level, ensuring no gaps and no double-counting.

## MaxLTXFileInfo Tests

`TestCompactor_MaxLTXFileInfo` tests three paths:
- **WithFiles** — returns the info for the file with the highest `MaxTXID`, not just the last file listed
- **NoFiles** — returns zero value without error when no files exist
- **WithCache** — confirms the `CacheGetter`/`CacheSetter` hooks are called and that cached values are returned on subsequent calls without listing remote files

## Retention Tests

Each retention method has both a normal test and a `_RetentionDisabled` variant:

- `TestCompactor_EnforceRetentionByTXID` — creates files spanning TXIDs 1–3, enforces retention to TXID 2, and confirms only TXID 3 remains
- `TestCompactor_EnforceL0Retention` — creates files with timestamps, enforces a time-based retention window, and confirms old files are deleted
- `TestCompactor_EnforceSnapshotRetention` — creates snapshots at different timestamps and confirms only recent ones are kept, but always at least one
- `*_RetentionDisabled` variants — set `RetentionEnabled = false` and confirm that remote files are not deleted (local cleanup may still occur)

## Verification Tests

- `TestCompactor_VerifyLevelConsistency` — creates a contiguous sequence of files and confirms no error; also tests with a gap in TXIDs and confirms an error is returned
- `TestCompactor_CompactWithVerification` — enables `VerifyCompaction = true`, runs a compaction, and confirms the verification check runs without error on a clean compaction

## Test Helpers

- `createTestLTXFile(t, client, level, minTXID, maxTXID)` — writes a minimal valid LTX file to the replica client at the given level and TXID range
- `createTestLTXFileWithTimestamp(...)` — same but with a specific timestamp for time-based retention tests
- `containsString(s, substr)` — simple string containment check used in error message assertions

## Known Gaps

No test for the `LocalFileOpener` code path — all tests use a bare file replica client without local file hooks.