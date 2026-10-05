---
{
  "title": "SQLite Database Seed Generator for Replication Testing",
  "summary": "PopulateCommand fills a SQLite database to a target size with randomized data, configurable table count, row size, batch size, and index density. It is used to create realistic-sized databases for replication performance and restore benchmarking.",
  "concepts": [
    "database seeding",
    "SQLite population",
    "WAL pressure",
    "batch inserts",
    "index density",
    "page size",
    "crypto/rand",
    "size parsing",
    "restore benchmarking",
    "replication testing"
  ],
  "categories": [
    "litestream",
    "SQLite",
    "testing",
    "tooling"
  ],
  "source_docs": [
    "a67a7b7cc269d803"
  ],
  "backlinks": null,
  "word_count": 389,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Replication performance is heavily influenced by database size and page count. A 1MB database restores in milliseconds; a 10GB database may take minutes. `PopulateCommand` creates databases of controlled size so tests can exercise restore latency, WAL checkpoint pressure, and compaction throughput at scale without requiring production data.

## Size Parsing

`parseSize` converts human-readable strings like `"500MB"` or `"1GB"` to byte counts. This prevents the common mistake of passing raw byte counts to flags and misinterpreting whether the value is MB or bytes. The parser handles `KB`, `MB`, `GB`, and `TB` suffixes case-insensitively.

## Database Population Strategy

`populateDatabase` loops until `getDatabaseSize` reports the on-disk file size meets or exceeds `targetBytes`. For each table, `populateTable` inserts rows in batches using transactions:

```go
for batch := 0; rows < rowCount; batch++ {
    tx, _ := db.BeginTx(ctx, nil)
    for i := 0; i < batchSize && rows < rowCount; i++ {
        // INSERT random payload
    }
    tx.Commit()
}
```

Batching is critical: single-row transactions on SQLite are extremely slow because each commit forces a WAL sync. Batching amortizes that cost and produces realistic WAL segment sizes for the replication layer to process.

## Random Payload

Row content is generated with `crypto/rand` to prevent SQLite's page compression from making the database smaller than expected. Uncompressible binary data ensures each row consumes exactly `RowSize` bytes on disk, making the total database size predictable.

## Index Density

`IndexRatio` controls the fraction of columns that get a secondary index. Indexes significantly affect WAL size and checkpoint time because index pages must also be journaled on every write. Testing with varied `IndexRatio` values catches cases where Litestream's compaction incorrectly handles index-page-heavy WAL segments.

## Page Size

SQLite's page size (`PRAGMA page_size`) must be set before any data is written and cannot be changed afterward. The default 4KB page size is fine for most workloads, but databases with large rows benefit from 8KB or 16KB pages. `PopulateCommand` exposes `PageSize` so testers can verify that Litestream handles non-default page sizes correctly during restore.

## Known Gaps

- The size check uses the raw file size (`os.Stat`), not the SQLite page count, so free-page fragmentation can cause the loop to overshoot or undershoot the target if VACUUM has been run.
- There is no progress reporting for very large targets; populating a 10GB database produces no output until complete.