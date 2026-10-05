---
{
  "title": "Test Suite: VFS Time Travel Queries and PRAGMA Introspection",
  "summary": "time_travel_test.go verifies that the Litestream VFS can pin queries to a specific timestamp or transaction ID, enabling historical reads against the replica. It also validates the `litestream_txid`, `litestream_lag`, and relative-time PRAGMA interfaces.",
  "concepts": [
    "time travel",
    "VFS timestamp pinning",
    "litestream_txid",
    "litestream_lag",
    "relative time",
    "PRAGMA introspection",
    "LTX metadata",
    "historical queries",
    "MVCC",
    "optimistic concurrency"
  ],
  "categories": [
    "testing",
    "litestream",
    "VFS",
    "time travel",
    "test"
  ],
  "source_docs": [
    "9333ab9a4c568643"
  ],
  "backlinks": null,
  "word_count": 373,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Time travel — querying a replica as it appeared at a past point in time — is a differentiating feature of the Litestream VFS. It enables use cases like debugging "what did this row look like yesterday?" without restoring a snapshot. The tests verify that timestamp pinning works correctly and that the PRAGMA interface for introspection returns accurate values.

## TestVFS_TimeTravelFunctions

This test calls `GoLitestreamSetTime` (via the VFS's exported function interface) to pin the VFS connection to a specific timestamp, then issues a SELECT. It verifies:

1. Rows inserted after the pinned timestamp are not visible.
2. Rows inserted before the pinned timestamp are visible.
3. Calling `GoLitestreamResetTime` restores the connection to the latest state.

The test uses `fetchLTXCreatedAt` to get the exact timestamp of a committed LTX file, then pins to a timestamp between two commits to verify the boundary behavior.

## TestVFS_PragmaLitestreamTxid

Verifies that `PRAGMA litestream_txid` returns the transaction ID of the LTX snapshot currently visible to the connection. This is used by applications to implement optimistic concurrency — if the txid changes between two reads, the application knows the replica has advanced and may need to re-read.

## TestVFS_PragmaLitestreamLag

Verifies that `PRAGMA litestream_lag` returns a non-negative duration in milliseconds representing how far behind the VFS is from the latest replicated transaction. A lag of 0 means the VFS is fully caught up; a large lag indicates network or storage delays.

## TestVFS_PragmaRelativeTime

Verifies that `litestream_set_time('-5m')` (a relative timestamp expression) correctly resolves to "5 minutes ago" relative to the current replica clock. This allows queries like "show me the state from 1 hour ago" without computing absolute timestamps in the application.

## fetchLTXCreatedAt Helper

`fetchLTXCreatedAt` lists LTX files from the replica client, reads the metadata timestamp from the first file, and returns it. This requires the LTX file to have been written with a timestamp metadata field — the ABS client stores this in `litestreamtimestamp`, and the file client stores it in extended attributes or a sidecar.

## Known Gaps

- The relative-time parsing (e.g., `"-5m"`) is tested only for minutes. Hours, days, and seconds are not explicitly tested.
- `fetchLTXCreatedAt` blocks indefinitely if no LTX files exist; there is no timeout on the listing call within the helper.