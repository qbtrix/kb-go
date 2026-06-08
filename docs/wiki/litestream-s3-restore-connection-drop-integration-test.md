---
{
  "title": "Litestream S3 Restore Connection Drop Integration Test",
  "summary": "Tests that Litestream can recover a database restore operation after mid-flight S3 connection resets, using Toxiproxy to inject network faults against a MinIO backend. Validates the entire fault-injection workflow: Docker networking, proxy control via HTTP API, and restored row-count verification.",
  "concepts": [
    "S3 restore",
    "connection drop",
    "Toxiproxy",
    "fault injection",
    "MinIO",
    "Docker networking",
    "integration test",
    "WAL segments",
    "TCP reset",
    "replica client",
    "litestream restore",
    "go-sqlite3"
  ],
  "categories": [
    "integration-testing",
    "resilience",
    "storage",
    "litestream",
    "test"
  ],
  "source_docs": [
    "122e73489e93fac1"
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

## Overview

This integration test (`TestRestore_S3ConnectionDrop`) exists to prove that Litestream's restore path is resilient to transient TCP connection resets on S3-compatible storage. Without this guard, a flaky cloud connection during a large restore could corrupt the restored database or leave it incomplete with no error surfaced to the caller.

## Infrastructure Setup

The test constructs an isolated Docker network containing two containers:

- **MinIO** — an S3-compatible object store that holds the replica data.
- **Toxiproxy** — a programmable network proxy from Shopify that sits between Litestream and MinIO.

```go
networkName := startDockerNetwork(t)
minioName := startMinioContainerForProxy(t, networkName)
toxiproxyName, toxiproxyAPIPort, toxiproxyProxyPort := startToxiproxyContainer(t, networkName)
```

Using a dedicated Docker network ensures containers can address each other by name while the test host reaches them on randomized host ports. The random port assignment (`-p 0:8474`) prevents collisions when tests run in parallel.

## Data Preparation

Before triggering the fault, the test:

1. Creates a 100 MB database with `db.Populate("100MB")`.
2. Starts Litestream replication and waits 5 seconds.
3. Inserts 5 rows of 256 KB blobs post-snapshot so that at least some WAL segments must be fetched during restore.

The large blob insertion (`insertLargeRows`) uses `randomblob(?)` — this is intentional. Random content defeats any HTTP-level compression that might mask a truncated transfer.

## Fault Injection

The `toxiproxyClient` struct wraps the Toxiproxy REST API:

- `createProxy` — registers MinIO behind the proxy.
- `addResetPeerToxic` — injects a `reset_peer` toxic that sends a TCP RST after a configurable timeout (200 ms here).
- `removeToxic` — clears the fault so the restore can complete.

The timing is deliberate: restore starts asynchronously in a goroutine, the toxic fires 200 ms later (mid-download), then is removed 400 ms after that. This window is sized to interrupt a large S3 GET in progress without permanently blocking the restore.

```go
go func() { restoreErr <- db.Restore(restorePath) }()
time.Sleep(200 * time.Millisecond)
proxyClient.addResetPeerToxic(t, "minio", "reset-connection", 200)
time.Sleep(400 * time.Millisecond)
proxyClient.removeToxic(t, "minio", "reset-connection")
```

## Verification

`verifyRestoredRowCount` opens the restored SQLite file with go-sqlite3 and asserts exactly 5 rows exist in the `drop_test` table. A count mismatch would mean the restore either missed WAL segments or applied them incorrectly after the retry.

## Known Gaps

- The Toxiproxy image is configurable via `LITESTREAM_TOXIPROXY_IMAGE` but defaults to a pinned version (`2.5.0`). There is no check that the running image matches the expected API version, so a stale local cache could silently use a different proxy.
- Sleep-based synchronization (`time.Sleep(5 * time.Second)`) is used to wait for replication to settle. A flaky CI environment with resource contention could cause these waits to be too short.
- The test only verifies a single TCP reset scenario. Scenarios like packet loss, partial reads, or multiple sequential faults are not covered.