---
{
  "title": "Test Suite: Comprehensive VFS Replica Behavior and Fault Scenarios",
  "summary": "main_test.go is the primary VFS test file, covering correctness, concurrency, fault injection, and performance across 25+ test functions and a benchmark. It defines a rich set of mock client wrappers that simulate network latency, eventual consistency, file descriptor limits, OOM conditions, and page index corruption.",
  "concepts": [
    "VFS testing",
    "fault injection",
    "eventual consistency",
    "file descriptor limit",
    "page index OOM",
    "corruption recovery",
    "concurrent reads",
    "latency simulation",
    "mock client",
    "benchmark",
    "SQLite VFS"
  ],
  "categories": [
    "testing",
    "litestream",
    "VFS",
    "fault tolerance",
    "test"
  ],
  "source_docs": [
    "89ec2b0675330496"
  ],
  "backlinks": null,
  "word_count": 439,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The VFS layer has complex failure modes that do not surface in unit tests: concurrent readers racing with live replication, storage backends returning stale listings before eventual consistency catches up, page index structures running out of memory mid-query. This test file systematically covers those scenarios with focused test cases.

## Mock Client Architecture

Six specialized client wrappers implement `litestream.ReplicaClient` for specific failure scenarios:

| Wrapper | Simulates |
|---|---|
| `testVFS` + `injectingFile` | Filesystem-level read errors injected per path |
| `latencyReplicaClient` | Network latency on every `OpenLTXFile` call |
| `eventualConsistencyClient` | `LTXFiles` returns empty for the first N calls |
| `observingReplicaClient` | Counts `LTXFiles` calls to verify polling behavior |
| `fdLimitedReplicaClient` | Caps concurrent open file descriptors |
| `flakyLTXClient` | Fails `LTXFiles` on configurable calls |
| `oomPageIndexClient` | Returns error from `OpenLTXFile` at a specific offset |
| `corruptingPageIndexClient` | Returns corrupt bytes from `OpenLTXFile` |

This layered approach means each test exercises exactly one failure mode, making failures easy to diagnose.

## Key Test Scenarios

**`TestVFS_WaitsForInitialSnapshot`**: verifies the VFS blocks until at least one LTX file is available before allowing the first query. Without this, a query racing with the first replication event would read an empty database.

**`TestVFS_S3EventualConsistency`**: uses `eventualConsistencyClient` to simulate S3's eventual consistency. After a `PutObject`, a `ListObjects` call may not immediately return the new object. The VFS must retry listing until the file appears.

**`TestVFS_FileDescriptorBudget`**: uses `fdLimitedReplicaClient` to verify the VFS respects a cap on simultaneously open LTX files. Exceeding the OS fd limit would cause the VFS to fail all queries unexpectedly.

**`TestVFS_PageIndexOOM`**: injects an error from `OpenLTXFile` mid-page-index load to verify the VFS does not leave a partially-loaded index in memory, which would cause subsequent reads to return wrong pages.

**`TestVFS_PageIndexCorruptionRecovery`**: injects corrupt bytes into an `OpenLTXFile` response and verifies the VFS discards the corrupted index and rebuilds it from a clean read, rather than propagating the corruption to SQLite.

**`BenchmarkVFS_LargeDatabase`**: benchmarks query latency on a large seeded database, providing a regression signal for VFS performance changes.

## `hookedReadCloser`

A helper that wraps an `io.ReadCloser` and fires a hook function exactly once on `Close()`. Used by `TestVFS_PartialLTXUpload` to simulate a connection reset mid-download — the hook fires when SQLite closes the page reader, verifying the VFS handles incomplete reads without corrupting its page cache.

## Known Gaps

- `TestVFS_LongRunningTxnStress` is listed in the AST but not described in the source excerpt — its exact behavior is inferred from its name.
- The benchmark does not report P99 latency, only average, which may hide tail latency regressions that matter most for interactive query workloads.