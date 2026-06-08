---
{
  "title": "Test Suite for Litestream File-Backend Library Example",
  "summary": "TestLibraryExampleFileBackend is an integration test that exercises the complete write-replicate-restore cycle using the file backend. It writes a row, waits for LTX files to appear in the replica directory, closes the store, then restores into a new path to verify data survives.",
  "concepts": [
    "integration test",
    "Litestream restore",
    "LTX files",
    "file backend",
    "WAL mode",
    "busy-wait polling",
    "store lifecycle",
    "cleanup guard",
    "compaction levels",
    "SQLite"
  ],
  "categories": [
    "testing",
    "litestream",
    "integration",
    "SQLite",
    "test"
  ],
  "source_docs": [
    "86cc51a2443fff37"
  ],
  "backlinks": null,
  "word_count": 454,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Unit tests can verify that individual functions return the right values, but they cannot verify that Litestream actually wrote LTX files to disk and that those files are sufficient to reconstruct the database. This integration test fills that gap by running the real storage layer against a temporary directory.

## Test Flow

1. A temporary directory holds both the primary database and the file replica — `t.TempDir()` ensures cleanup regardless of test outcome.
2. The Litestream store is opened with L0 + L1 compaction levels.
3. A table is created and one row is inserted.
4. `waitForLTXFiles` polls the replica directory until at least one `.ltx` file appears under `ltx/0/`. This busy-wait is necessary because LTX flush is asynchronous — the test cannot simply check immediately after the insert.
5. The SQL connection is closed before the store, ensuring the WAL is fully checkpointed.
6. The store is closed. The `!errors.Is(err, sql.ErrTxDone)` guard handles a known benign race where the store's internal transaction is already done by the time `Close` returns.
7. A new `ReplicaClient` is constructed pointing at the same replica directory and `Restore` is called. If this succeeds without error, the complete cycle is validated.

## `waitForLTXFiles` Design

```go
func waitForLTXFiles(replicaPath string, timeout time.Duration) error {
    deadline := time.Now().Add(timeout)
    for time.Now().Before(deadline) {
        matches, _ := filepath.Glob(filepath.Join(replicaPath, "ltx", "0", "*.ltx"))
        if len(matches) > 0 { return nil }
        time.Sleep(100 * time.Millisecond)
    }
    return fmt.Errorf("timeout waiting for ltx files in %s", replicaPath)
}
```

The 100ms poll interval and 5s timeout are chosen to be fast enough for CI while giving the background monitor time to detect the WAL and flush the first LTX file. A shorter poll would burn CPU; a shorter timeout would cause flaky failures on slow CI runners.

## Cleanup Guard

The test uses a `closed bool` flag and a `t.Cleanup` function to ensure the store is closed even if the test fails mid-way. Without this guard, a failed assertion before the explicit `store.Close(ctx)` call would leak the background goroutines until the test binary exits.

## WAL Configuration

`openAppDB` explicitly sets `PRAGMA journal_mode = wal` and `PRAGMA busy_timeout = 5000` via `ExecContext` rather than in the DSN. This is equivalent to the DSN approach in the basic example but shows the alternative that works when the DSN cannot be easily modified.

## Known Gaps

- The test does not verify that the restored database contains the expected row — it only confirms that `Restore` did not return an error. A query against the restored database would make the test more robust.
- The L1 compaction interval (10 seconds) is longer than the test timeout (10 seconds), so L1 compaction is never exercised. The test only validates L0 replication.