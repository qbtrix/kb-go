---
{
  "title": "DB: Core SQLite Replication Engine",
  "summary": "The central type in the litestream library, `DB` manages a single SQLite database file — monitoring its WAL, converting WAL changes into LTX files, triggering compaction, enforcing retention, and coordinating with a replica client. It exposes sync, checkpoint, snapshot, and shutdown operations, and tracks replication state through Prometheus metrics.",
  "concepts": [
    "DB",
    "WAL sync",
    "LTX encoder",
    "checkpoint",
    "TXID",
    "ReplicaClient",
    "Compactor",
    "Prometheus metrics",
    "read lock",
    "snapshot",
    "writeLTXFromWAL",
    "writeLTXFromDB",
    "verifyAndSync",
    "detectFullCheckpoint",
    "SyncAndWait",
    "SyncStatus",
    "ResetLocalState",
    "MetaPath",
    "EnsureExists"
  ],
  "categories": [
    "litestream",
    "replication",
    "sqlite",
    "storage"
  ],
  "source_docs": [
    "5e036a1da107d6a7"
  ],
  "backlinks": null,
  "word_count": 579,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`DB` is the most complex type in litestream. It wraps a SQLite database file and provides continuous replication by reading the WAL, converting changes to the LTX format, writing LTX files locally and to a remote replica, and managing the local compaction cache. All replication state is maintained in a meta directory alongside the database file.

## Lifecycle

`Open()` initializes the database handle, acquires a read lock on the SQLite file to prevent external checkpoints from racing with litestream's sync, enables WAL mode if not already set, and starts background monitoring goroutines. `Close(ctx)` flushes any pending sync to the replica with a configurable retry loop (`syncReplicaWithRetry`) before returning — this shutdown sync loop is the guarantee that no committed transactions are lost when the daemon stops.

## WAL Synchronization

`Sync(ctx)` is the core replication step:

1. `verify(ctx)` reads the current WAL state, detects salt changes (indicating a full checkpoint occurred), and determines the current database page count
2. `sync(ctx, checkpointing, info)` converts WAL pages not yet in LTX format into a new LTX file via `writeLTXFromWAL()` or `writeLTXFromDB()` (used when a checkpoint has occurred)
3. The resulting LTX file is written to local storage and uploaded to the replica via `syncReplicaWithRetry`
4. Metrics are updated: WAL size, DB size, TXID, sync count, sync error count, sync duration

`verifyAndSync()` coordinates these steps and handles the checkpoint-interleaved case: if a checkpoint happens mid-sync, the WAL is re-read from the database pages rather than the WAL file.

## Checkpoint Strategy

Litestream holds a read transaction on the database at all times to prevent SQLite from checkpointing pages that litestream hasn't yet replicated. When the WAL reaches `MinCheckpointPageN` pages and `CheckpointInterval` has elapsed, litestream triggers a `PASSIVE` checkpoint itself. After enough checkpoints, a `TRUNCATE` checkpoint is attempted to reset the WAL file to zero bytes.

`detectFullCheckpoint()` detects when SQLite itself performed a checkpoint (salt values in the WAL header change), triggering a snapshot to ensure the replica has a consistent baseline.

## LTX File Generation

`writeLTXFromWAL()` reads uncommitted WAL pages and writes them into an LTX file using the `ltx.Encoder`. `writeLTXFromDB()` reads from the database pages directly when a checkpoint has occurred and the WAL has been reset. Both methods track which pages have been seen via a `pageMap` to handle WAL pages that supersede earlier versions of the same page.

## Compaction and Retention

`Compact(ctx, dstLevel)` delegates to the embedded `Compactor` after setting up the local file hooks (`LocalFileOpener`, `LocalFileDeleter`, `CacheGetter`, `CacheSetter`). `EnforceSnapshotRetention()` and `EnforceL0Retention()` delegate similarly. `Snapshot()` creates a level-9 (snapshot) LTX file from the current database state.

## Metrics

All metrics use Prometheus and are registered with `promauto`:

- `db_size_bytes` and `wal_size_bytes` gauges
- `total_wal_bytes` counter (cumulative bytes through the WAL)
- `txid` gauge (current transaction ID)
- `sync_total`, `sync_error_total`, `sync_seconds_total` counters
- `checkpoint_total`, `checkpoint_error_total`, `checkpoint_seconds_total` counter vectors (by mode: passive, full, truncate)
- L0 retention gauges for min/max TXID after retention enforcement

## SyncStatus and SyncAndWait

`SyncStatus(ctx)` reports whether the database is in sync with its replica by comparing the local LTX TXID against the replica's last known TXID. `SyncAndWait(ctx)` triggers a sync and blocks until the replica acknowledges receipt, used by the `sync -wait` CLI command.

## Known Gaps

`ResetLocalState(ctx)` removes the LTX directory but does not remove the Prometheus metrics, which may briefly show stale values after a reset. The `EnsureExists(ctx)` method creates the database file if it does not exist, but its interaction with the read lock acquisition is subtle and not fully documented.