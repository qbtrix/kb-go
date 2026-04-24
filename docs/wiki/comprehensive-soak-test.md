---
{
  "title": "Comprehensive Soak Test",
  "summary": "A long-running soak test (default 2 hours, 2 minutes in short mode) that exercises replication, snapshots, compaction at multiple levels, checkpoints, and restoration under aggressive settings. Uses the integration+soak build tags to isolate it from normal CI.",
  "concepts": [
    "soak test",
    "long-running test",
    "compaction levels",
    "snapshot interval",
    "checkpoint",
    "replication",
    "restoration",
    "GetTestDuration",
    "build tags",
    "integration+soak",
    "endurance testing"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "6dfa59dd2b2adedb"
  ],
  "backlinks": null,
  "word_count": 305,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestComprehensiveSoak` is a stress-endurance test, not a functional test. It runs Litestream with tight compaction intervals (30 s, 1 m, 5 m, 15 m, 30 m) and frequent snapshots (every 10 m) for an extended period. The goal is to catch bugs that only appear under sustained load: goroutine leaks, file handle exhaustion, accumulating disk usage from missed retention cleanup, or compaction errors that only occur after many cycles.

## Build Tags

The `//go:build integration && soak` constraint means this test only runs when both `integration` and `soak` are in the `-tags` flag. This prevents it from appearing in normal `go test ./...` runs or standard integration test suites.

## Duration Control

`GetTestDuration` reads the requested duration from a test-level variable, defaulting to 2 hours. Passing `-test.short` reduces this to 2 minutes, which runs quickly enough to be used in a pre-flight check before triggering the full overnight run.

## What Is Exercised

- **Continuous replication**: every WAL write is synced to the file replica on a short interval.
- **Snapshot generation**: full snapshots are taken every 10 minutes, testing that snapshot writes do not block replication.
- **Multi-level compaction**: five compaction levels are active simultaneously, stressing the file management paths that move and delete LTX files.
- **Checkpoint operations**: the SQLite WAL is periodically checkpointed, testing that Litestream recovers correctly after checkpoint resets.
- **Restoration**: at the end of the run, the database is restored from the replica and validated against the source.

## Known Gaps

- The test uses `RequireBinaries` so it depends on a pre-built `litestream` binary. If the binary is built from a different commit than the source, the soak result may not represent the current code.
- There is no metrics export during the run — operator must inspect the test log for compaction timing or file counts.