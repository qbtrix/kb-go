---
{
  "title": "Overnight S3 Soak Test",
  "summary": "An 8-hour overnight soak test that replicates to real AWS S3 using credentials from environment variables. Validates long-term S3 replication stability, compaction, and restore with actual S3 API semantics and durability guarantees.",
  "concepts": [
    "overnight soak",
    "AWS S3",
    "long-duration",
    "credential rotation",
    "rate limiting",
    "8-hour test",
    "eventual consistency",
    "restore accuracy",
    "aws s3 ls",
    "S3_BUCKET",
    "GetTestDuration",
    "soak \u0026\u0026 aws"
  ],
  "categories": [
    "testing",
    "integration",
    "storage backends",
    "test"
  ],
  "source_docs": [
    "30e2a42ffcec898f"
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

`TestOvernightS3Soak` is the highest-fidelity soak test in the suite. It runs against production AWS S3, covering scenarios that MinIO or file-based tests cannot reproduce: real eventual consistency windows, S3 rate limiting, IAM credential refresh, and multi-region routing.

## Build Tags

`integration && soak && aws` means this test runs only when all three tags are present. The `aws` tag explicitly prevents accidental execution in environments without S3 credentials.

## Credential Requirements

The test reads:
- `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` for authentication.
- `S3_BUCKET` for the target bucket.
- `AWS_REGION` (optional, defaults to `us-east-1`).

If credentials are absent the test skips via `RequireBinaries` or environment checks rather than failing with a cryptic AWS error.

## Duration and Short Mode

Default duration is 8 hours. `-test.short` reduces it to 1 hour. `GetTestDuration` reads both and applies the appropriate value.

## What Is Validated

- **Long-term stability**: Litestream must sustain replication for 8 hours without process crashes, goroutine leaks, or file handle exhaustion.
- **S3 rate limiting**: heavy compaction creates many API calls; the test verifies Litestream handles 429/503 responses with retry rather than failing.
- **Restore accuracy**: after 8 hours of writes and compaction, a full restore must produce a database with the same row count as the source.

## Metrics Logging

`logS3Metrics` uses the AWS CLI (`aws s3 ls`) to count objects in the bucket, logged periodically during the run. `getRowCountFromPath` reads row counts from a SQLite file without an active connection for safe comparison.

## Known Gaps

- The test does not verify that IAM credentials are rotated correctly if the session token expires during an 8-hour run (relevant for role-based credentials with short TTLs).
- S3 versioning is not enabled on the test bucket; if Litestream accidentally deletes production data via `DeleteAll`, there is no recovery path.