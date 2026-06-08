---
{
  "title": "WebDAV Replica Client Tests",
  "summary": "Tests for the Litestream WebDAV ReplicaClient using an in-process fake WebDAV server, covering error handling for missing paths, range-request fallback behavior, and partial content responses. The fake server is minimal by design to isolate only the behaviors the client must handle correctly.",
  "concepts": [
    "WebDAV",
    "ReplicaClient",
    "fakeWebDAVServer",
    "Range request",
    "PROPFIND",
    "httptest",
    "LTX",
    "litestream",
    "OpenLTXFile",
    "DeleteAll",
    "range fallback",
    "in-process test server"
  ],
  "categories": [
    "testing",
    "litestream",
    "networking",
    "storage",
    "test"
  ],
  "source_docs": [
    "2e7cd97bfc40785c"
  ],
  "backlinks": null,
  "word_count": 595,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Testing a WebDAV client against a real server requires external infrastructure and produces flaky tests. This file instead uses an in-process `fakeWebDAVServer` that implements exactly the subset of WebDAV needed to exercise the `ReplicaClient`'s code paths deterministically.

## Fake WebDAV Server

`fakeWebDAVServer` is an `http.Handler` backed by a `map[string][]byte` keyed on URL path. It supports:

- **PUT** — Stores the request body
- **GET** — Returns stored content, with Range request support unless `ignoreRange` is set
- **DELETE** — Removes the file; returns 404 if `deleteReturn404[path]` is true
- **PROPFIND** — Returns a minimal WebDAV XML listing for stored paths
- **MKCOL** — Returns 201 Created unconditionally

The `ignoreRange` flag is the key enabler for `TestReplicaClient_OpenLTXFile_RangeFallback`: by telling the fake server to ignore Range headers, the test confirms the client correctly falls back to full-stream-then-skip when the server does not honor range requests.

`newFakeWebDAVServer` wraps the struct in an `httptest.NewServer`, giving a local HTTP listener the client can target without any network configuration.

## Test Cases

**`TestReplicaClient_Type`** — Confirms `Type()` returns `"webdav"`. This string is used as a Prometheus metric label; a wrong value would misroute metrics.

**`TestReplicaClient_Init_RequiresURL`** — Creates a client with no URL set and calls `Init`. Expects an error. Without this guard, the underlying HTTP client would attempt a request to an empty host and return a confusing network error.

**`TestReplicaClient_DeleteAll_NotFound`** — Points the client at a non-existent path and calls `DeleteAll`. The fake server's PROPFIND handler returns 404. The test confirms `DeleteAll` returns `nil` rather than an error — a missing directory during teardown is not a failure.

**`TestReplicaClient_LTXFiles_PathNotFound`** — Calls `LTXFiles` when the PROPFIND returns 404 and confirms an empty iterator is returned rather than an error. This is the fresh-deployment case: no LTX files exist yet, so listing should succeed with zero results.

**`TestReplicaClient_OpenLTXFile_RangeFallback`** — Sets `ignoreRange = true` on the fake server, uploads an LTX payload, then calls `OpenLTXFile` with both `offset` and `size` set. Because the server ignores the Range header and returns the full file, the client must skip `offset` bytes manually. The test reads the result and verifies it matches the expected suffix of the original payload.

**`TestReplicaClient_OpenLTXFile_OffsetOnly`** — Similar but passes only `offset` (size = 0), exercising the skip-only code path where no range request is attempted at all.

## Helper Functions

**`newTestReplicaClient(baseURL)`** constructs a `ReplicaClient` with the test server URL, a synthetic path prefix, and no credentials.

**`buildLTXPayload(minTXID, maxTXID, payload)`** constructs a minimal valid LTX file around arbitrary payload bytes. This ensures `WriteLTXFile` can parse and store the file correctly, and that the content survives a round-trip through the fake server's byte map.

**`parseRange(header, size)`** is a local HTTP Range header parser used by the fake server's GET handler. It extracts the `start` and `end` byte positions from a `"bytes=N-M"` header, returning 416 for malformed inputs. This avoids pulling in a full HTTP range library for test purposes.

## Design Rationale

The fake server deliberately omits authentication, TLS, and multi-resource PROPFIND responses. Real WebDAV servers have complex behaviors in these areas, but the client tests are scoped to the HTTP-layer behaviors the `ReplicaClient` code actually branches on — range support, 404 handling, and MKCOL idempotency. Keeping the fake minimal means test failures are unambiguous: if a test breaks, it is because the client code changed, not because the fake server's behavior drifted.

## Known Gaps

There are no tests for `WriteLTXFile` failure paths (e.g., MKCOL fails, PUT returns a server error) or for `DeleteLTXFiles` with a mix of existing and missing files. The `failingWriteClient` pattern from `vfs_write_test.go` is not replicated here.