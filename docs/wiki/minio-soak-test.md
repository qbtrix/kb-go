---
{
  "title": "MinIO Soak Test",
  "summary": "A long-duration soak test (default 2 hours) that replicates a SQLite database to a local MinIO S3-compatible server running in Docker. Validates sustained replication, compaction, and restore against an actual S3 API surface without AWS costs.",
  "concepts": [
    "MinIO",
    "soak test",
    "Docker",
    "S3-compatible",
    "long-duration",
    "bucket metrics",
    "compaction",
    "file count",
    "GenerateLoad",
    "mc ls",
    "ephemeral container",
    "restore verification"
  ],
  "categories": [
    "testing",
    "integration",
    "storage backends",
    "test"
  ],
  "source_docs": [
    "381f8362ea68a735"
  ],
  "backlinks": null,
  "word_count": 299,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestMinIOSoak` runs Litestream against a temporary MinIO Docker container for an extended period. The `integration && soak && docker` build tags ensure it is isolated from both regular integration runs and the `aws`-tagged overnight S3 test.

## Why MinIO

Real AWS S3 has costs, rate limits, and eventual consistency behaviors that make sustained automated testing expensive and slow. MinIO provides identical S3 API semantics on a local container, making it safe to run frequent compaction cycles and large data volumes without cost concerns.

## Test Flow

1. `RequireDocker` skips the test if Docker is unavailable.
2. `StartMinioTestContainer` launches an ephemeral MinIO instance on a random port.
3. The test creates a database, populates it, and starts Litestream configured to replicate to the MinIO bucket.
4. The load loop writes rows continuously for the configured duration using `db.GenerateLoad`.
5. At regular intervals, `logMinIOMetrics` calls `countMinIOLTXFiles` via `docker exec mc ls` to count objects in the bucket, logging file count trends.
6. After the load phase, Litestream is stopped, the database is restored, and row counts are compared between source and restored database using `getRowCountFromPath`.

## Metrics Logging

`logMinIOMetrics` uses the MinIO client (`mc`) via `docker exec` to list bucket contents. Logging file counts over time allows post-hoc analysis of whether compaction is running as expected and whether L0 files are being pruned.

`countMinIOLTXFiles` parses `mc ls` output to count `.ltx` files. It uses prefix filtering to count per-level, distinguishing L0 accumulation from L1/L2 compacted files.

## Short Mode

With `-test.short`, the duration drops from 2 hours to 30 minutes, making it practical as a pre-commit gate on a developer machine.

## Known Gaps

- The MinIO container does not have durability configured (data is in-memory by default for the ephemeral test container), so storage-level durability is not tested.