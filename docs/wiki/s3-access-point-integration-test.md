---
{
  "title": "S3 Access Point Integration Test",
  "summary": "Integration test that verifies Litestream can replicate to an S3 access point using a MinIO container as a LocalStack substitute. Tests ARN-based URL addressing, bucket creation, replication, and restore end-to-end without requiring real AWS resources.",
  "concepts": [
    "S3 Access Point",
    "ARN URL",
    "MinIO",
    "LocalStack",
    "ForcePathStyle",
    "createBucket",
    "clearBucket",
    "waitForObjects",
    "compareRowCounts",
    "findUserTable",
    "WriteS3AccessPointConfig",
    "Docker"
  ],
  "categories": [
    "testing",
    "integration",
    "storage backends",
    "test"
  ],
  "source_docs": [
    "f917076dde5e2eaa"
  ],
  "backlinks": null,
  "word_count": 332,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestS3AccessPointLocalStack` validates the S3 Access Point URL path — `s3://arn:aws:s3:region:account:accesspoint/name` — using a local MinIO container configured via the AWS SDK directly. This avoids the cost of provisioning a real AWS access point for every test run.

## Why Access Points Matter

S3 Access Points provide scoped bucket access with per-point policies. Organizations use them to grant fine-grained access to specific prefixes within a bucket without IAM user proliferation. Litestream must support ARN-based URLs to work in environments where users are given an access point ARN rather than a direct bucket name.

## Test Infrastructure

The test:
1. Calls `RequireBinaries` and `RequireDocker` to skip if either is missing.
2. Starts a MinIO container via `StartMinioTestContainer`.
3. Creates a bucket using `newMinioS3Client` and `createBucket` — helper functions that configure an S3 SDK client pointed at the local MinIO endpoint with `ForcePathStyle=true`.
4. Writes a config file using `WriteS3AccessPointConfig` with the access point ARN-style URL.
5. Starts Litestream and generates write load.
6. Waits for objects to appear in the bucket via `waitForObjects`.
7. Stops Litestream, restores, and compares row counts with `compareRowCounts`.

## Bucket Management

`clearBucket` deletes all objects before the test to ensure a clean state. Without this, leftover objects from a prior failed run could cause the restore to include stale data.

`waitForObjects` polls the bucket until at least one `.ltx` object appears. This is necessary because Litestream's first sync may be delayed by the database initialization path.

## Row Count Comparison

`compareRowCounts` opens both source and restored databases with the SQLite driver, finds the first user-created table using `findUserTable` (which skips `sqlite_*` internal tables), and compares row counts. Using the first user table rather than a hardcoded name makes the test schema-agnostic.

## Known Gaps

- MinIO does not implement real S3 Access Point ARN routing — it treats the ARN URL as a regular path-style key. The test validates URL parsing and replication mechanics but not the actual access point policy enforcement that real AWS would apply.