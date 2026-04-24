---
{
  "title": "SQLite Boundary Condition Integration Tests",
  "summary": "Integration tests that verify Litestream handles SQLite edge cases around the 1 GB database size boundary and the SQLite lock page for non-standard page sizes. These guard against silent data corruption at structural limits that are invisible in normal unit tests.",
  "concepts": [
    "lock page",
    "1 GB boundary",
    "SQLite page size",
    "integration test",
    "boundary condition",
    "page 262145",
    "data corruption",
    "page numbering",
    "RequireBinaries",
    "containsAny"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "0523aafbf0fb8476"
  ],
  "backlinks": null,
  "word_count": 377,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file contains two integration tests targeting SQLite structural boundaries that require large or carefully sized databases to reproduce. Both tests are gated by `RequireBinaries` and skip in `-short` mode because they take non-trivial setup time.

## 1 GB Lock Page Boundary

`Test1GBBoundary` creates a SQLite database with 4 KB pages and grows it past 1 GB. SQLite reserves page 262,145 (the 1 GB boundary for 4 KB pages) as a "lock page" — it is never written to and must be skipped during replication. If Litestream naively streams all page numbers sequentially, it would either include the lock page (corrupting the replica) or fail to increment past it (stalling replication).

The test populates enough data to cross the boundary, runs Litestream replication, then restores and validates the replica. The `containsAny` helper checks error messages for known lock-page error strings, distinguishing expected lock-page skips from genuine errors.

## Lock Page with Non-Default Page Sizes

`TestLockPageWithDifferentPageSizes` runs the same scenario with multiple page sizes (512 B, 1 KB, 4 KB, 8 KB, 16 KB, 32 KB, 64 KB). The lock page falls at different byte offsets depending on page size:

- 512 B pages: lock page at 2,097,153
- 4 KB pages: lock page at 262,145
- 64 KB pages: lock page at 16,385

Without this test, a fix for 4 KB page databases might not handle 512 B or 64 KB databases where the boundary is at a different page number.

## Helper Functions

`containsAny` returns true if a string contains any of a list of substrings — used to match multiple possible lock-page error message variants across SQLite versions. `contains` is the single-match variant. `anySubstring` is an alias used for clarity in assertion messages.

## Why These Are Integration Tests

Generating a 1+ GB database is too slow for unit tests and requires the real `litestream` binary for restore validation. The `integration` build tag gates these to a dedicated test run rather than normal CI.

## Known Gaps

- The test currently only tests file-based replicas; S3 or SFTP replicas are not covered by this boundary check.
- There is no test for WAL files that cross the lock-page boundary (e.g., a transaction whose WAL segment spans pages on both sides of page 262,145).