---
{
  "title": "Overnight File-Based Replication Tests",
  "summary": "Overnight endurance tests using local file system replica storage. Provides a baseline for long-duration stability without cloud dependencies, using both a single-scenario file replication test and a comprehensive multi-scenario variant.",
  "concepts": [
    "overnight test",
    "file replication",
    "endurance test",
    "8-hour",
    "100MB initial data",
    "compaction",
    "snapshot",
    "PrintTestSummary",
    "GetTestDuration",
    "integrity check",
    "long build tag"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "6257a02fc775654e"
  ],
  "backlinks": null,
  "word_count": 284,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

These tests use the `integration && long` build tags and run for 8 hours by default. Because they replicate to the local filesystem rather than a cloud provider, they can run without credentials and are cheaper to execute repeatedly. They are the recommended first step when investigating stability regressions.

## TestOvernightFile

A straightforward endurance test:
1. Creates a database and populates it with 100 MB of initial data.
2. Starts Litestream replication to a temp directory.
3. Generates continuous load (writes + checkpoint cycles) for the full duration.
4. Stops Litestream, restores the database, validates row counts match.

Using 100 MB of initial data rather than starting empty tests Litestream's behavior when it must snapshot a large existing database before replication can begin.

## TestOvernightComprehensive

Runs the same scenario as `TestOvernightFile` but adds:
- Multiple compaction levels enabled simultaneously.
- Snapshot generation every 10 minutes.
- Periodic integrity checks on the live database during the run.

This is the "everything on" test — it exercises the full compaction, snapshot, and retention pipeline over an 8-hour window, not just the raw replication path.

## PrintTestSummary

Both tests defer `db.PrintTestSummary` to log the total runtime, write count, and final replica file count. This creates an audit trail for each overnight run that can be compared against previous runs to detect performance regressions.

## Short Mode Behavior

`GetTestDuration` returns 8 hours by default or a shorter configurable duration. Tests call `t.Skip` in `-short` mode to avoid running in normal CI where wall-clock time is constrained.

## Known Gaps

- Neither test exercises WAL corruption recovery (intentional bit-flips or truncation) — that scenario is covered only by the fuzz test and a separate recovery integration test.