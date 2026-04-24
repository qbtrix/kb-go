---
{
  "title": "Replica — Core Replication and Restore Engine",
  "summary": "The Replica type orchestrates continuous replication from a SQLite database to a remote backend via a ReplicaClient, and implements the full restore pipeline including snapshot selection, WAL segment application, point-in-time targeting, and database integrity verification.",
  "concepts": [
    "Replica",
    "continuous replication",
    "Sync",
    "Restore",
    "CalcRestorePlan",
    "TXID sidecar",
    "WriteTXIDFile",
    "integrity check",
    "v2 protocol",
    "v3 protocol",
    "WAL segment",
    "snapshot",
    "point-in-time restore",
    "AutoRecoverEnabled"
  ],
  "categories": [
    "replication",
    "restore",
    "core",
    "litestream"
  ],
  "source_docs": [
    "7a2d782527f2bcc2"
  ],
  "backlinks": null,
  "word_count": 358,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

The `Replica` is the heart of litestream's operational logic. It connects a watched `DB` to a `ReplicaClient` backend and manages two distinct workflows: continuous replication (uploading new LTX files as transactions commit) and restore (downloading and applying LTX files to reconstruct the database at a given point in time or TXID).

## Continuous Replication

`Start` launches a background monitor goroutine that calls `Sync` on the configured `SyncInterval`. `Sync` reads the current replica position from the backend, identifies new LTX files produced since the last sync, and uploads them via `uploadLTXFile`. The position is tracked with `calcPos` which examines the highest TXID present on the replica.

`AutoRecoverEnabled` controls whether the replica automatically attempts recovery if it detects its local position has fallen behind in a way that cannot be caught up incrementally (e.g., after a VACUUM that resets the WAL sequence).

## Restore Pipeline

The restore path supports two protocol versions:

### v2 (Level-Based LTX)
`Restore` selects the best snapshot (highest level LTX file covering the target TXID), then iterates through level-0 incremental files to apply any transactions not covered by the snapshot. `CalcRestorePlan` determines the exact set of files needed. `findBestLTXSnapshotForTimestamp` selects the closest snapshot without exceeding the target timestamp.

### v3 (Generation-Based WAL)
`RestoreV3` downloads a full WAL-mode snapshot from a generation UUID, then applies WAL segments up to the target timestamp using `applyWALSegmentsV3`.

`shouldUseV3Restore` selects between the two protocols based on what data is available in the backend.

## TXID Sidecar Files

`WriteTXIDFile` and `ReadTXIDFile` manage a `<db>-txid` sidecar file that records the highest TXID applied during a restore. This lets follow mode (`follow`) resume from the correct position after a process restart without re-reading the entire LTX history.

## Integrity Checking

`checkIntegrity` runs SQLite's `PRAGMA integrity_check` or `PRAGMA quick_check` on the restored database. Running this after restore catches corruption before the application opens the file.

## Known Gaps

`ValidationError` is defined but the `ValidateLevel` method's internal validation loop details are not fully visible in the AST extract. The `fillFollowGap` method handles the case where incremental LTX files have a gap that must be filled by fetching a compacted file from a higher level.