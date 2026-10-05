---
{
  "title": "Concurrent Operations Integration Tests",
  "summary": "Integration tests that stress Litestream under concurrent SQLite write patterns: rapid checkpoints, unbounded WAL growth, concurrent reader/writer combinations, and SQLite busy-timeout behavior. Guards against replication failures caused by SQLite locking interactions.",
  "concepts": [
    "concurrent writes",
    "WAL checkpoint",
    "WAL growth",
    "busy_timeout",
    "SQLITE_BUSY",
    "shadow WAL",
    "file size",
    "replication under load",
    "goroutine",
    "integration test"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "987af04b9beffef5"
  ],
  "backlinks": null,
  "word_count": 269,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

These tests exercise the boundary between SQLite's WAL mode locking and Litestream's shadow WAL tracking under concurrent load. Unit tests cannot cover these scenarios because they require actual filesystem locking semantics and real SQLite connections.

## Rapid Checkpoints

`TestRapidCheckpoints` writes data and triggers `PRAGMA wal_checkpoint(TRUNCATE)` in a tight loop while Litestream is running. The scenario tests whether Litestream's shadow WAL reader correctly handles checkpoints that happen faster than sync intervals, potentially skipping generations. Without this, a checkpoint could truncate WAL data that Litestream has not yet replicated.

## WAL Growth

`TestWALGrowth` writes aggressively without checkpointing and measures the WAL file size over time. The test asserts that the WAL does not grow without bound — Litestream's periodic checkpoint should bound WAL size. `getFileSize` reads the filesystem-level file size to bypass SQLite's PRAGMA, which may cache stale values.

## Concurrent Operations

`TestConcurrentOperations` runs multiple goroutines performing reads and writes simultaneously against the same database while Litestream replicates. It verifies that replication continues without error under reader/writer lock contention and that the restored database contains all committed writes. This is the closest approximation to a production workload in the integration suite.

## Busy Timeout

`TestBusyTimeout` configures SQLite's `busy_timeout` PRAGMA and verifies that Litestream respects it rather than failing immediately when it encounters a locked database. Without a busy timeout, Litestream's internal sync could fail with `SQLITE_BUSY` on a heavily written database and enter an error loop.

## Known Gaps

- Tests do not cover the case where a checkpoint fails due to an active reader holding a snapshot (i.e., `SQLITE_BUSY_SNAPSHOT`), which can stall WAL growth indefinitely in some configurations.