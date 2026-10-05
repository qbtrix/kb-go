---
{
  "title": "Test Suite for Google Cloud Storage Replica Client",
  "summary": "Tests the GCS replica client against a local fake-gcs-server instance to verify that LTX files written by WriteLTXFile can be fully read back via OpenLTXFile without data loss.",
  "concepts": [
    "fake-gcs-server",
    "fakestorage",
    "GCS emulator",
    "round-trip test",
    "WriteLTXFile",
    "OpenLTXFile",
    "LTX header",
    "in-process server",
    "integration test",
    "byte fidelity"
  ],
  "categories": [
    "testing",
    "cloud",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "4db8f1041041805d"
  ],
  "backlinks": null,
  "word_count": 271,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

This test file exercises the GCS replica client end-to-end using `fakestorage.Server`—an in-process GCS emulator—to avoid dependencies on real Google Cloud credentials. It covers the round-trip fidelity of the write/read cycle, which is the most important correctness property of any replica backend.

## Test Infrastructure

`setupTestClient` creates a `fakestorage.Server` with `NoListener: true` (in-memory only, no TCP port), creates a bucket named `litestream-test`, and wires the fake server's HTTP client directly into the `ReplicaClient`'s internal `client` field. This approach bypasses the `Init` path to inject a controlled client without needing real GCP credentials. The base path is set to `"integration"` to simulate a non-root replica path.

`ltxTestData` assembles a minimal valid LTX binary blob with a real `ltx.Header` (version 1, 4096-byte page size) and appends caller-provided payload bytes. This ensures the written data is structurally valid LTX so the client's header-peeking logic in `WriteLTXFile` does not error.

## Full Round-Trip Test

`TestReplicaClient_OpenLTXFileReadsFullObject` writes an LTX file via `WriteLTXFile`, then opens it with `OpenLTXFile` at offset 0 and size 0 (meaning: read the full object), reads all bytes, and performs an exact byte comparison. The purpose is to ensure that:

1. The `TeeReader` header-extraction in `WriteLTXFile` does not drop bytes.
2. The GCS client library correctly reconstructs the stream through the fake server.
3. No encoding or metadata transformation corrupts the payload.

## Known Gaps

Only one functional test is present. There is no coverage for partial reads (offset > 0), the `useMetadata` timestamp path, concurrent writes, or the `DeleteAll` / `DeleteLTXFiles` operations. The `fakestorage` server is not fully GCS-spec-compliant, so some edge cases (conditional writes, resumable uploads) are not exercised here.