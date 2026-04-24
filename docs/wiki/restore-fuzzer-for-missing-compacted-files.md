---
{
  "title": "Restore Fuzzer for Missing Compacted Files",
  "summary": "A Go fuzz test that stress-tests the restore path when compacted LTX files have been partially or fully deleted from a live replica. It seeds random write patterns and timing variations to surface crashes or incorrect behavior that deterministic unit tests miss.",
  "concepts": [
    "fuzzing",
    "compaction",
    "restore",
    "LTX files",
    "L0",
    "L1",
    "L2",
    "CompactionLevels",
    "SnapshotInterval",
    "timing race",
    "missing files",
    "Store",
    "file iterator"
  ],
  "categories": [
    "testing",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "5689eaa2ca59dc2d"
  ],
  "backlinks": null,
  "word_count": 364,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`FuzzRestoreWithMissingCompactedFile` exercises a specific failure mode: compaction collapses multiple L0 LTX files into a higher-level file and deletes the originals. If a restore begins after compaction starts but before it completes, some expected files may be absent. The fuzz test probes whether the restore logic handles this gracefully — returning a correct error rather than panicking or silently producing a corrupted database.

## Why Fuzzing Here

Deterministic tests for this scenario are hard to write because the failure window depends on timing between the compaction goroutine, the sync goroutine, and the restore call. A fixed sleep value either never triggers the race or triggers it every time, making the test fragile. By running with seed-driven randomness and varying the `MonitorInterval`, `SyncInterval`, compaction intervals, and `SnapshotInterval`, the fuzzer explores the timing space without hardcoding specific delays.

## Test Setup

The fuzzer seeds three initial values (`1`, `2`, `3`) to anchor the corpus. Each fuzz iteration:

1. Creates a fresh database with a 20 ms monitor interval.
2. Attaches a `Store` with three compaction levels — L0 (continuous), L1 (50 ms), L2 (150 ms) — and a 200 ms snapshot interval.
3. Opens the database and the SQL connection, creates a test table.
4. Drives writes using the seed-derived random number generator to vary write timing and count.
5. Attempts a restore while compaction may be ongoing.

The `testing.Short()` guard skips the fuzz test in `-short` mode since it is too slow for routine CI.

## Failure Modes Targeted

- **Panic on nil iterator**: if the file iterator returns no results for a TXID range the caller expects, a careless dereference panics.
- **Incorrect error wrapping**: a missing-file error from the storage backend should propagate as a recoverable error, not cause the restore to silently succeed with partial data.
- **Compaction-restore race**: L1 compaction may delete L0 files while restore is scanning them, causing an `os.ErrNotExist` mid-stream.

## Known Gaps

The fuzz test creates a table but the write loop details depend on the full source not shown in the AST summary. The test is not run in standard `go test` mode — it requires `go test -fuzz=FuzzRestoreWithMissingCompactedFile` to explore beyond the seed corpus.