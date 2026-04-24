---
{
  "title": "Compactor: LTX File Merging, Retention, and Verification",
  "summary": "Implements the `Compactor` struct that performs the actual work of merging LTX files from one compaction level into the next, enforcing retention policies that delete old files, and optionally verifying TXID contiguity after each merge. The Compactor operates through the `ReplicaClient` interface, making it usable with any storage backend.",
  "concepts": [
    "Compactor",
    "LTX merge",
    "ReplicaClient",
    "CompactionLevels",
    "ErrNoCompaction",
    "CacheGetter",
    "CacheSetter",
    "EnforceSnapshotRetention",
    "EnforceRetentionByTXID",
    "EnforceL0Retention",
    "VerifyLevelConsistency",
    "RetentionEnabled",
    "LocalFileOpener",
    "Prometheus counter",
    "TXID contiguity"
  ],
  "categories": [
    "litestream",
    "compaction",
    "storage",
    "retention"
  ],
  "source_docs": [
    "0163533e2e2e4ecb"
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

`Compactor` is the engine behind litestream's multi-tier compaction. It is backend-agnostic: all operations go through the `ReplicaClient` interface, so the same code handles local filesystem, S3, GCS, SFTP, and any other backend. The `DB` type embeds a `Compactor` and provides local-file hooks; the VFS layer uses a bare `Compactor` without local caching.

## Compaction: Merging LTX Files Upward

`Compact(ctx, dstLevel)` merges all LTX files from `dstLevel-1` that are newer than the most recent file already at `dstLevel`. The merge:

1. Determines the high-water mark at the destination level (`MaxLTXFileInfo`)
2. Seeks to the next TXID in the source level
3. Opens each source file (preferring the local cache via `LocalFileOpener` when available)
4. Concatenates the readers and feeds them through `ltx.NewCompactor` which handles the LTX merge format
5. Writes the merged result to the destination level via `client.WriteLTXFile()`
6. Optionally verifies TXID contiguity at the destination level

Returns `ErrNoCompaction` when there are no source files newer than the destination's high-water mark. Callers use this to skip unnecessary work.

## Cache Integration

The `CacheGetter` and `CacheSetter` callbacks allow the `DB` type to cache the max LTX file info per level in memory. Without caching, every compaction call must list files from the replica client, which involves a network round-trip for remote backends. The cache trades memory for reduced latency and API call cost.

The cache is write-through: after a new file is written, `CacheSetter` is called with the updated info. Reads check the cache first and only fall back to the remote listing if no cached value exists.

## Retention Enforcement

Three methods handle different retention tiers:

### `EnforceSnapshotRetention(ctx, retention)`
Deletes snapshots older than `retention`. Keeps the most recent snapshot regardless of age, preventing the edge case where an aggressive retention policy deletes all snapshots and makes point-in-time restore impossible.

### `EnforceRetentionByTXID(ctx, level, txID)`
Deletes files at a given level whose `MaxTXID` is below the specified transaction ID. Used after compaction to remove source-level files that have been merged and are no longer needed for restore.

### `EnforceL0Retention(ctx, retention)`
Deletes level-0 (raw WAL) files older than `retention`. L0 files accumulate fastest and represent the most granular data, so they are cleaned up separately with their own retention interval.

When `RetentionEnabled` is false, all three methods skip remote deletion. Local file cleanup still occurs via `LocalFileDeleter` — this allows operators to use cloud provider lifecycle policies for remote retention while still keeping the local LTX directory tidy.

## Post-Compaction Verification

`VerifyLevelConsistency(ctx, level)` iterates all files at a level and checks that their TXID ranges are contiguous (no gaps, no overlaps). It is called after compaction when `VerifyCompaction` is true. Verification failures increment `CompactionVerifyErrorCounter` (a Prometheus counter) and log an error but do not return an error — this is intentional because a verification failure is a diagnostic signal, not a reason to abort ongoing replication.

## Known Gaps

No test for the `LocalFileOpener` fallback path specifically — tests use a file replica client which always returns remote files. If `LocalFileOpener` returns an unexpected error type (other than `os.ErrNotExist`), the behavior is not documented.