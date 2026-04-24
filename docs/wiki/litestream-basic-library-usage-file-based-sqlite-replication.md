---
{
  "title": "Litestream Basic Library Usage: File-Based SQLite Replication",
  "summary": "This example demonstrates the minimal API surface needed to embed Litestream in a Go program, replicating a SQLite database to the local filesystem. It shows the correct initialization order (Store → SQLite connection → application logic) and how to handle graceful shutdown.",
  "concepts": [
    "Litestream",
    "SQLite replication",
    "WAL mode",
    "LTX files",
    "compaction levels",
    "Store",
    "ReplicaClient",
    "file backend",
    "graceful shutdown",
    "embedded replication",
    "Go library"
  ],
  "categories": [
    "litestream",
    "SQLite",
    "replication",
    "example"
  ],
  "source_docs": [
    "43e56edb570f7e63"
  ],
  "backlinks": null,
  "word_count": 474,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Most Litestream documentation covers the standalone daemon. This example exists for teams that want replication baked into their Go binary — no sidecar process, no external config file. Embedding Litestream gives tighter lifecycle control and avoids the operational burden of coordinating a separate process.

## Initialization Order

The ordering of operations is deliberate and must not be reversed:

1. **Create `litestream.NewDB`** — wraps the file path but does not open it yet.
2. **Create `file.NewReplicaClient`** — points at the replica directory.
3. **Wire replica to DB** — both `db.Replica` and `client.Replica` must be set so the store can route writes bidirectionally.
4. **Define compaction levels** — L0 is mandatory (raw LTX files); L1+ define compaction intervals. Without at least two levels the store will reject the configuration.
5. **`store.Open(ctx)`** — this opens all managed databases, starts background WAL monitors, and begins streaming LTX files to the replica.
6. **Open the application's own `*sql.DB`** — only after the store is open, so the WAL monitor is already watching before any writes occur. Opening the SQL connection first would allow writes to begin before Litestream has started capturing them.

## SQLite WAL Mode

`openAppDB` opens the database with `journal_mode=wal` and `busy_timeout=5000` embedded in the DSN. WAL mode is required for Litestream because it uses the WAL file as the change feed. The busy timeout prevents immediate failures when Litestream's background reader and the application writer briefly contend on the database.

## Schema Idempotency

`initSchema` uses `CREATE TABLE IF NOT EXISTS`. This matters because on restart the database may already exist (either local or restored from replica). An unconditional `CREATE TABLE` would fail on restart, so the guard ensures the example is safe to run multiple times against the same file.

## Graceful Shutdown

The main loop selects on both a ticker channel and a signal channel (`SIGINT`/`SIGTERM`). On shutdown, `store.Close` is called with a fresh `context.Background()` rather than the already-cancelled context — this is intentional. Closing with a cancelled context would abort in-flight LTX flushes, potentially leaving the replica behind by one or more transactions.

## Compaction Levels

```go
levels := litestream.CompactionLevels{
    {Level: 0},
    {Level: 1, Interval: 10 * time.Second},
}
```

L0 stores raw per-transaction LTX files with no compaction. L1 compacts L0 files every 10 seconds into a merged LTX covering a range of transactions. This reduces the number of objects a restore operation must download at the cost of a small write amplification on the replica side.

## Known Gaps

- The example does not demonstrate restore-on-startup. A freshly deployed instance would start with an empty database rather than recovering from the replica. See the S3 example for the production pattern.
- Error handling in `insertRow` is logged but does not stop the ticker, so transient write errors are silently retried on the next tick rather than surfaced to the operator.