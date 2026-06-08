---
{
  "title": "SQLite Database Shrinker for Replication Stress Testing",
  "summary": "ShrinkCommand deletes a configurable percentage of rows from all tables in a SQLite database and optionally runs VACUUM and/or checkpoint afterward. It is used to test how Litestream handles database shrinkage, free-page reuse, and the resulting WAL patterns.",
  "concepts": [
    "SQLite shrink",
    "database deletion",
    "VACUUM",
    "WAL checkpoint",
    "free pages",
    "B-tree rebalancing",
    "PASSIVE checkpoint",
    "TRUNCATE checkpoint",
    "replication stress",
    "Litestream testing"
  ],
  "categories": [
    "litestream",
    "SQLite",
    "testing",
    "tooling"
  ],
  "source_docs": [
    "37c5b10667dd19c8"
  ],
  "backlinks": null,
  "word_count": 347,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Most replication tests focus on growth — inserting data and verifying it arrives at the replica. But production databases also shrink when users delete records, and shrinkage creates a different WAL pattern: free-list pages, B-tree rebalancing, and (after VACUUM) a rewritten database file. `ShrinkCommand` lets testers reproduce these patterns deliberately.

## Deletion Strategy

`deleteFromTable` deletes `DeletePercentage`% of rows from each table by sampling row IDs:

```go
delete_count = total_rows * (DeletePercentage / 100)
DELETE FROM table WHERE rowid IN (SELECT rowid FROM table ORDER BY RANDOM() LIMIT ?)
```

Random deletion by rowid is more realistic than `DELETE FROM table LIMIT N` because it creates non-contiguous free pages in the B-tree, which is harder for both SQLite and Litestream to handle than deleting a contiguous range.

## VACUUM

When `--vacuum` is set, `runVacuum` runs `VACUUM` after deletion. VACUUM rewrites the entire database file from scratch, eliminating free pages and compacting the B-tree. From Litestream's perspective, this looks like a complete database replacement — every page is "changed" and must be replicated. VACUUM stress-tests Litestream's ability to handle large single-transaction WAL segments.

## Checkpoint Modes

`runCheckpoint` calls `PRAGMA wal_checkpoint(<mode>)`. The supported modes reflect SQLite's own checkpoint modes:

| Mode | Behavior |
|---|---|
| `PASSIVE` | Writes what it can without blocking readers |
| `FULL` | Waits for readers to finish, then checkpoints |
| `RESTART` | Like FULL but restarts WAL from beginning |
| `TRUNCATE` | Like RESTART but also truncates the WAL file |

Running `TRUNCATE` after shrink + vacuum creates a minimal WAL state, useful for testing restore from a clean-slate checkpoint.

## Table Discovery

`getTableList` queries `sqlite_master` for `type='table'` excluding SQLite internal tables (those starting with `sqlite_`). This ensures the command works against any schema without hardcoded table names.

## Known Gaps

- Deletion is per-table sequentially, not in a single transaction. If the process is interrupted mid-run, the database is left in a partially shrunk state with no record of which tables were processed.
- VACUUM and checkpoint are run unconditionally on all tables rather than being configurable per-table.