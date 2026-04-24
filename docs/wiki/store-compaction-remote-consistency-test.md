---
{
  "title": "Store Compaction Remote Consistency Test",
  "summary": "Tests that the Litestream compaction pipeline handles eventually-consistent remote stores correctly by simulating a replica client where newly written objects are not immediately visible. Verifies the system does not read partially-written compacted snapshots and gracefully retries.",
  "concepts": [
    "eventual consistency",
    "delayedReplicaClient",
    "partial snapshot",
    "compaction",
    "LTX files",
    "remote store",
    "available-at delay",
    "buildPartialSnapshot",
    "error handling",
    "Store",
    "mock client"
  ],
  "categories": [
    "testing",
    "compaction",
    "litestream",
    "test"
  ],
  "source_docs": [
    "272c536ecae6503c"
  ],
  "backlinks": null,
  "word_count": 317,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test exercises a subtle distributed systems property: object stores like S3 sometimes serve a newly-written object as unavailable for a brief window after the write completes. If compaction reads from a replica immediately after writing a higher-level file, it might see a partial or absent file.

`TestStore_CompactDB_RemotePartialRead` uses a `delayedReplicaClient` that withholds newly written files for a configurable duration (`delay`). This simulates the eventual consistency window without requiring network access.

## delayedReplicaClient

The mock client stores files in an in-memory map (`files []*delayedFile`). Each `delayedFile` has a `createdAt` and `availableAt` timestamp. `LTXFiles` filters the list to only return files whose `availableAt` is in the past. `OpenLTXFile` returns a "not found" error for files not yet available.

The `partial` flag on `delayedFile` represents a file that was written incompletely — only the first portion of pages is stored. `buildPartialSnapshot` creates such a file: a valid LTX header with fewer pages than a full snapshot. This is the failure case where a compaction write to the remote store was interrupted after some bytes landed but before the file was finalized.

## What the Test Validates

- **No panic on partial read**: if compaction reads a partial snapshot and encounters unexpected EOF mid-file, it should return an error, not panic.
- **No incorrect success**: compaction should not promote a partial file to a higher compaction level, which would make data permanently inaccessible.
- **Retry after availability**: once the delay expires and the file becomes available, compaction should succeed on the next cycle.

## waitForAvailability

The `waitForAvailability` helper blocks until `time.Now()` exceeds `availableAt` for all pending files, allowing tests to advance time-based assertions without arbitrary sleeps.

## Known Gaps

- The `delayedReplicaClient` does not simulate 503/429 throttle responses — it uses `os.ErrNotExist` to represent unavailability, which maps cleanly to Litestream's not-found handling but may miss cases where the store returns a transient HTTP error instead of a 404.