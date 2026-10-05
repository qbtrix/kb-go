---
{
  "title": "Test Suite for File Replica Client — Temp File Cleanup and Protocol Coverage",
  "summary": "Tests the file backend's ReplicaClient implementation with a focus on temp-file leak prevention during failed writes, basic property accessors, and the full set of v3 protocol methods (generations, snapshots, WAL segments).",
  "concepts": [
    "temp file cleanup",
    "failAfterReader",
    "disk full simulation",
    "WriteLTXFile",
    "LTX header",
    "PreApplyChecksum",
    "GenerationsV3",
    "SnapshotsV3",
    "WALSegmentsV3",
    "file replica client",
    "resource hygiene"
  ],
  "categories": [
    "testing",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "728cdd473237b117"
  ],
  "backlinks": null,
  "word_count": 325,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

This test file guards against regressions in the file replica client. Its most important concern is resource hygiene: when a write fails partway through (disk full, I/O error), no `.tmp` files should be left behind to accumulate on disk. The secondary concern is that v3 generation-based methods return correct empty results when the backing directories do not yet exist.

## Temp File Cleanup Tests

`TestReplicaClient_WriteLTXFile_ErrorCleanup` is the centerpiece and covers three scenarios:

- **DiskFull**: A `failAfterReader` emits 50 bytes then returns a `"no space left on device"` error. After the write attempt, `findTmpFiles` walks the directory tree recursively looking for any file ending in `.tmp`. The test fails if any are found.
- **SuccessNoLeaks**: A complete, valid LTX payload is written successfully. The test confirms no `.tmp` files remain *and* that the final named file exists at the expected path.
- **MultipleErrors**: Five consecutive writes all fail. This guards against a scenario where the first failure creates a `.tmp` file that is cleaned up, but a later failure's cleanup somehow affects other state.

The `failAfterReader` helper is a careful implementation that tracks byte position and injects the configured error precisely after `n` bytes—not after `n` *reads*. This is necessary because Go's `io.Copy` issues variable-size read calls; a naive counter could misfire.

## LTX Test Data Helpers

`createLTXHeader` and `createLTXData` build minimal valid LTX binary blobs by populating the required `ltx.Header` fields (version, page size, commit count, TXID range, timestamp) and marshalling to binary. The `PreApplyChecksum` field is set to `ltx.ChecksumFlag` for non-snapshot files, matching the production invariant that incremental LTX files carry a checksum of the state they apply on top of.

## v3 Protocol Tests

`TestReplicaClient_GenerationsV3`, `SnapshotsV3`, `WALSegmentsV3`, `OpenSnapshotV3`, and `OpenWALSegmentV3` all verify the "directory doesn't exist" and "empty directory" cases return zero results rather than errors. This matters for newly provisioned replicas where no data has been written yet—returning an error for a missing directory would prevent restore from ever starting.