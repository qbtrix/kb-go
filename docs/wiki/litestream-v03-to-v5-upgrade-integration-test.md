---
{
  "title": "Litestream v0.3 to v5 Upgrade Integration Test",
  "summary": "Verifies that data replicated with the old Litestream v0.3.x binary can be fully restored by the current v5 binary, covering the complete upgrade path from the legacy snapshot and WAL format to the new LTX-based format. This test is the primary regression guard for backward-compatibility between storage generations.",
  "concepts": [
    "upgrade path",
    "backward compatibility",
    "v0.3.x format",
    "LTX format",
    "restore",
    "WAL mode",
    "ReplicaClientV3",
    "generations",
    "snapshot",
    "SIGTERM",
    "binary compatibility",
    "litestream"
  ],
  "categories": [
    "integration-testing",
    "litestream",
    "compatibility",
    "data-migration",
    "test"
  ],
  "source_docs": [
    "97498a35a885881a"
  ],
  "backlinks": null,
  "word_count": 469,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestUpgrade_V3ToV5` exists because Litestream v5 introduced a fundamentally new storage format (LTX-based compaction levels) that is incompatible with the v0.3.x snapshot-and-WAL layout. Without an explicit upgrade test, a refactoring of the v3 reader layer could silently break the ability to restore data that was replicated before the upgrade — a data-loss scenario that would be invisible until a customer tried to recover.

## Prerequisites

The test requires two binaries:

- **`LITESTREAM_V3_BIN`** — path to the v0.3.x binary, set via environment variable. If absent, the test skips rather than failing. This design allows the test to be added to CI before the v3 binary is checked in.
- **Current v5 binary** — located at a well-known relative path (`../../bin/litestream`) via `getBinaryPath`.

Both binaries are verified to exist and have the executable bit set before the test proceeds. This prevents confusing "permission denied" errors deep in the test from masking a mis-configured environment.

## Test Phases

The test is structured in two explicit phases:

### Phase 1: v0.3.x Replication

1. Create a WAL-mode SQLite database and insert rows into an `upgrade_test` table.
2. Write a Litestream v3 config pointing to a local file replica.
3. Start the v0.3.x binary as a subprocess and allow it to replicate.
4. Insert additional rows tagged with `phase = 'v3'` while replication is running.
5. Send SIGTERM to the v0.3.x process and wait for it to exit.

This produces a replica directory in the v0.3.x format (`generations/` with `snapshots/` and `wal/` subdirectories).

### Phase 2: v5 Restore

1. Use the current v5 binary to run `litestream restore` against the v3 replica path.
2. Open the restored database and query row counts.
3. Use the `litestream` Go package directly to verify that `ReplicaClientV3` can enumerate the v3 generations, confirming the compatibility layer is exercised.

A row count mismatch between source and restored databases is a hard failure. The test also checks for the presence of both `phase = 'v3'` rows and any rows inserted before replication started, ensuring the full snapshot-plus-WAL replay chain works.

## Why WAL Mode

The test explicitly sets `PRAGMA journal_mode=WAL`. The old v0.3.x format was designed around WAL checkpoints as its primary unit of replication. Rollback journal mode would produce a structurally different replica, potentially masking bugs in the WAL replay path.

## Known Gaps

- Only the local file replica path is tested; the v3 S3 reader is not exercised. An S3-backed upgrade scenario would require the Docker infrastructure from the connection-drop test.
- The test does not verify that v5 cannot accidentally write v3 format files after a restore, leaving open the risk of a mixed-format replica if the upgrade process is interrupted.
- There is no test for the reverse path (restoring v5 data with a v3 binary), which would confirm the format change is intentionally one-way.