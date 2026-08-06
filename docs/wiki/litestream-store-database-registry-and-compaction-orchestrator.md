---
{
  "title": "Litestream Store: Database Registry and Compaction Orchestrator",
  "summary": "The Store is the top-level container that manages a collection of replicated databases, drives background compaction, snapshot generation, retention enforcement, and validation. It coordinates the lifecycle of all registered DBs and exposes a control surface for dynamic registration and sync.",
  "concepts": [
    "Store",
    "CompactionLevels",
    "L0 retention",
    "snapshot",
    "validation",
    "compaction monitor",
    "heartbeat",
    "DBNotReadyError",
    "RegisterDB",
    "UnregisterDB",
    "EnableDB",
    "ShutdownSyncTimeout",
    "VerifyCompaction",
    "errgroup"
  ],
  "categories": [
    "replication",
    "compaction",
    "litestream"
  ],
  "source_docs": [
    "127799327524dd13"
  ],
  "backlinks": null,
  "word_count": 459,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`Store` owns a set of `*DB` instances and coordinates all background work through dedicated goroutines. It is the component that transforms individual per-database replication into a coherent system with multi-level compaction and retention policy.

## Database Registry

`RegisterDB` adds a database to the store's internal map keyed by absolute path. `UnregisterDB` removes it and initiates cleanup. `EnableDB` and `DisableDB` toggle active replication without removing the entry — useful when a database is temporarily unavailable. `FindDB` does a direct path lookup.

`SyncDB` triggers an on-demand sync for a specific path and optionally blocks (`wait=true`) until the sync completes. This is used by the control server's `/sync` endpoint.

## Compaction Levels

The `Store` is initialized with `CompactionLevels` that define the interval at which each level consolidates LTX files from the level below. `monitorCompactionLevel` runs one goroutine per compaction level, firing on `time.Ticker` and calling the per-DB compact method. This design allows each level to compact independently — L1 may compact every minute while L2 compacts every 10 minutes, without coupling their schedules.

## Snapshot Management

`SnapshotInterval` controls how often the store forces a full snapshot of each database. `SnapshotRetention` controls how many old snapshots to keep. `SnapshotLevel` returns the index of the highest compaction level, which is where snapshots are stored.

## L0 Retention

`monitorL0Retention` runs a background loop that deletes L0 files (raw WAL segments) older than `L0Retention`. L0 accumulates quickly under write-heavy workloads; without retention enforcement, disk and storage costs grow unbounded. `L0RetentionCheckInterval` controls how often this check runs.

## Validation

When `ValidationInterval` is set, the store periodically restores each database from its replica and compares it against the live database. This is an end-to-end correctness check that catches silent data corruption in the replication pipeline. `VerifyCompaction` enables a lighter check that validates compacted files without a full restore.

## Shutdown Sync

`ShutdownSyncTimeout` and `ShutdownSyncInterval` control behavior during `Close`: before stopping, the store attempts to drain pending writes by syncing all databases. A configurable timeout prevents shutdown from hanging indefinitely on a slow replica backend.

## Heartbeat

`HeartbeatCheckInterval` and `Heartbeat` support an external liveness probe. The store updates the heartbeat timestamp periodically; an external watchdog can check it to detect a stalled store.

## DBNotReadyError

`DBNotReadyError` is returned when an operation is attempted on a database that has not yet completed initialization. The `Is` method allows callers to use `errors.Is` for pattern matching. Without this typed error, callers cannot distinguish "not ready" from "does not exist" and would need to retry indiscriminately.

## Known Gaps

- `CompactionMonitorEnabled` can disable compaction monitoring entirely, but there is no per-level enable/disable — the flag is all-or-nothing.
- Heartbeat monitoring (`heartbeatMonitorRunning`) tracks whether the monitor goroutine is active but offers no API for the external watchdog to subscribe; it must poll.