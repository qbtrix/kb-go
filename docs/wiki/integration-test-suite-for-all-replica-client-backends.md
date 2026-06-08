---
{
  "title": "Integration Test Suite for All Replica Client Backends",
  "summary": "Comprehensive integration tests that run against every registered replica client backend, verifying LTX file write/read/delete operations, timestamp preservation, S3-specific upload configuration, PITR correctness with large file sets, and SFTP host key validation.",
  "concepts": [
    "integration test",
    "RunWithReplicaClient",
    "timestamp preservation",
    "PITR",
    "CalcRestorePlan",
    "S3 multipart",
    "SFTP host key",
    "out-of-order write",
    "timestamp filtering",
    "backend fan-out"
  ],
  "categories": [
    "testing",
    "integration",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "fbba6fcca8ef0477"
  ],
  "backlinks": null,
  "word_count": 276,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This is the top-level integration test file for replica client backends. Tests use `RunWithReplicaClient` to fan out across all backends enabled by the `-integration` flag. This means a single test function like `TestReplicaClient_LTX` exercises file, S3, GCS, SFTP, WebDAV, NATS, OSS, and other backends without backend-specific test code.

## Core CRUD Tests

- `TestReplicaClient_LTX`: Writes LTX files out of order, then reads them back in sorted TXID order. Out-of-order writes specifically test that backends sort by TXID rather than insertion order.
- `TestReplicaClient_WriteLTXFile`: Verifies written files are retrievable and have correct TXID metadata.
- `TestReplicaClient_OpenLTXFile`: Tests both full-file reads (offset=0, size=0) and partial reads with a non-zero offset.
- `TestReplicaClient_DeleteWALSegments`: Verifies that deleted files are no longer returned by the iterator.

## Timestamp Preservation

`TestReplicaClient_TimestampPreservation` writes multiple LTX files with distinct embedded timestamps, then calls `LTXFiles` with `useMetadata=true` and verifies that the returned `CreatedAt` matches the LTX header timestamp to within one millisecond. This is the key correctness test for PITR: if timestamps are wrong, restore picks the wrong files.

## S3-Specific Tests

- `TestReplicaClient_S3_UploaderConfig`: Verifies multipart upload thresholds and part sizes.
- `TestReplicaClient_S3_ErrorContext`: Checks that S3 errors include the bucket and key in the error message.
- `TestReplicaClient_S3_BucketValidation`: Empty bucket name returns a specific error before any network call.
- `TestReplicaClient_S3_UnsignedPayloadRejected`: Confirms that unsigned payload mode is rejected by real AWS endpoints.
- `TestReplicaClient_S3_ConcurrencyLimits`: Verifies that the semaphore correctly limits concurrent upload goroutines.

## PITR Regression Tests

`TestReplicaClient_PITR_ManyLTXFiles` writes hundreds of LTX files and verifies that `CalcRestorePlan` selects the correct snapshot and incrementals. This guards against a regression where the restore planner dropped files under large iteration counts. `TestReplicaClient_PITR_TimestampFiltering` verifies cross-level timestamp filtering.