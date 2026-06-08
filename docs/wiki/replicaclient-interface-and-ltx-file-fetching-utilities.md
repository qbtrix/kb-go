---
{
  "title": "ReplicaClient Interface and LTX File Fetching Utilities",
  "summary": "Defines the ReplicaClient interface that all storage backends must implement, and provides the utility functions for iterating, filtering, and fetching LTX file content including page index extraction and single-page decoding.",
  "concepts": [
    "ReplicaClient interface",
    "LTXFiles",
    "OpenLTXFile",
    "WriteLTXFile",
    "FindLTXFiles",
    "ErrStopIter",
    "FetchPageIndex",
    "FetchPage",
    "FetchLTXHeader",
    "useMetadata",
    "range read",
    "page index"
  ],
  "categories": [
    "interface",
    "replication",
    "core",
    "litestream"
  ],
  "source_docs": [
    "e3980ead226a085a"
  ],
  "backlinks": null,
  "word_count": 321,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is the contract layer between litestream's replication engine and its storage backends. Every backend (file, S3, GCS, NATS, SFTP, etc.) must satisfy `ReplicaClient`. The file also provides higher-level helpers that operate on top of the interface for common data access patterns.

## ReplicaClient Interface

The eight methods of `ReplicaClient` form a minimal CRUD surface for LTX files:

- `Type()` — a short string identifier used in logs and error messages.
- `Init(ctx)` — lazy connection establishment; must be idempotent.
- `LTXFiles(ctx, level, seek, useMetadata)` — returns an iterator starting at or after `seek`. The `useMetadata` flag controls whether expensive per-object metadata calls are made for accurate timestamps.
- `OpenLTXFile(ctx, level, minTXID, maxTXID, offset, size)` — range-read a specific LTX file. `offset` enables resumable reads; `size` caps the read for partial fetches.
- `WriteLTXFile(ctx, level, minTXID, maxTXID, r)` — atomic upload with metadata.
- `DeleteLTXFiles(ctx, a)` — batch delete.
- `DeleteAll(ctx)` — wipe the entire replica path.
- `SetLogger(logger)` — inject a namespaced logger.

## FindLTXFiles

`FindLTXFiles` wraps the iterator pattern: it opens a level iterator, calls a filter function on each item, and accumulates matches. The filter function can return `ErrStopIter` to terminate early without signaling a fatal error. This is used by restore logic to stop scanning once a file beyond the target TXID is found.

## Page Index Fetching

`FetchPageIndex` and `fetchPageIndexData` retrieve the LTX page index by fetching the tail of the file. LTX files store their page index at the end, so litestream can locate any page's byte offset without reading the full file. `fetchPageIndexData` estimates the index size with `DefaultEstimatedPageIndexSize` and grows the fetch if the index is larger than expected.

## FetchPage and FetchLTXHeader

`FetchPage` opens an LTX file at a computed offset and decodes a single SQLite page frame, returning the page number, data, and size. `FetchLTXHeader` reads just the file header to inspect metadata without downloading the full file.