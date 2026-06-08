---
{
  "title": "Litestream S3 Replication with Restore-on-Startup Pattern",
  "summary": "This example implements the production-grade Litestream embedding pattern: check for a local database, restore from S3 if absent, then start continuous replication. It demonstrates the correct restore-before-open lifecycle and environment-variable–based configuration for S3 credentials.",
  "concepts": [
    "S3 replication",
    "restore on startup",
    "ErrNoSnapshots",
    "Litestream",
    "SQLite",
    "ephemeral instances",
    "production pattern",
    "environment variables",
    "AWS credentials",
    "graceful shutdown",
    "compaction levels"
  ],
  "categories": [
    "litestream",
    "SQLite",
    "S3",
    "production",
    "example"
  ],
  "source_docs": [
    "fef907fc603a5739"
  ],
  "backlinks": null,
  "word_count": 407,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The basic file example works for local development but breaks in production environments where application instances are ephemeral (containers, spot instances, auto-scaling groups). When a new instance starts, it has no local database. Without a restore step, the application would initialize an empty database and immediately start overwriting the S3 replica with blank data.

This example codifies the production pattern that prevents that failure.

## Restore-Before-Open

`restoreIfNotExists` is called before `store.Open`. The ordering is critical:

- If the local DB file exists (`os.Stat` succeeds), the function returns immediately. This is the normal hot-restart path.
- If the file does not exist, it attempts `client.Init(ctx)` (which establishes the S3 connection) and then `replica.Restore(ctx, opt)`. Only if restore fails with `litestream.ErrNoSnapshots` — meaning no data exists in S3 yet — does it allow a fresh empty database to be created.

The `ErrNoSnapshots` check matters: on a brand-new deployment with an empty bucket, restore would fail because there is nothing to restore. Treating that error as fatal would prevent the first instance from ever starting. Treating it as a signal to proceed with an empty database is correct.

## S3 Client Configuration

```go
client := s3.NewReplicaClient()
client.Bucket = bucket
client.Path = path
client.Region = region
client.AccessKeyID = os.Getenv("AWS_ACCESS_KEY_ID")
client.SecretAccessKey = os.Getenv("AWS_SECRET_ACCESS_KEY")
```

All configuration comes from environment variables, making this pattern compatible with AWS IAM roles (where `AWS_ACCESS_KEY_ID` is empty and the SDK uses instance metadata). The explicit validation of `LITESTREAM_BUCKET` before any S3 calls prevents a confusing error from the AWS SDK about a missing bucket name.

## Compaction Levels

Two levels are configured: L0 (raw LTX per transaction) and L1 (10-second merge). In production, teams often add an L2 (hourly) and L3 (daily) level to reduce S3 object count and therefore restore latency. The example deliberately keeps it minimal.

## Graceful Shutdown

The `defer store.Close(context.Background())` uses a fresh context — not the signal-derived one — for the same reason as the basic example: a cancelled context would abort the final WAL flush, leaving the replica potentially one commit behind.

## Known Gaps

- No retry logic around the restore step. A transient S3 outage during startup would cause the instance to fail permanently rather than retrying.
- The example does not show how to pass S3-compatible endpoints (e.g., MinIO, Cloudflare R2), which require setting `client.Endpoint`.
- There is no health-check endpoint, so orchestrators cannot distinguish a healthy running instance from one stuck in the restore phase.