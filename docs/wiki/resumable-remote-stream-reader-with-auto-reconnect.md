---
{
  "title": "Resumable Remote Stream Reader with Auto-Reconnect",
  "summary": "Wraps a remote storage stream (S3, GCS, Tigris, etc.) and transparently reconnects from the last-read byte offset when the underlying connection is dropped mid-transfer. Designed to survive the idle-connection timeouts that cloud storage providers impose during long restore operations.",
  "concepts": [
    "resumable reader",
    "auto-reconnect",
    "idle connection timeout",
    "premature EOF",
    "S3 connection drop",
    "byte offset",
    "range request",
    "LTXFileOpener",
    "io.ReadFull",
    "restore operation",
    "cloud storage"
  ],
  "categories": [
    "io",
    "resilience",
    "replication",
    "litestream"
  ],
  "source_docs": [
    "511549dbc3829285"
  ],
  "backlinks": null,
  "word_count": 324,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## The Problem This Solves

During a Litestream restore, the compactor opens LTX file streams across potentially hundreds of files upfront, then processes pages in page-number order. An LTX file covering high page numbers may sit idle for minutes while low-numbered pages are processed from the snapshot. Cloud providers (S3, Tigris, Cloudflare R2) routinely close idle HTTP connections after 60–120 seconds, causing `io.ReadFull` to receive either a non-EOF error (connection reset, timeout) or a premature `io.EOF` (the server gracefully closes the connection before all bytes are delivered).

Without `ResumableReader`, every such idle-connection timeout would abort the entire restore with a fatal error, forcing the operator to restart from scratch.

## Failure Mode Detection

The reader tracks two conditions that indicate the stream broke:

1. **Non-EOF error**: Any error from `Read` that is not `io.EOF` — connection reset, timeout, TLS teardown.
2. **Premature EOF**: `io.EOF` returned when `offset < size` (where `size` is the known total file length from `FileInfo`). This catches servers that cleanly close an idle HTTP/1.1 connection after transferring only part of the object.

If `size` is 0 (unknown), premature EOF detection is disabled and a clean `io.EOF` is passed through to the caller as a legitimate end-of-stream.

## Reconnect Logic

On failure, `ResumableReader` closes the dead connection, sets `rc` to nil, and on the next `Read` call reopens the stream from `offset` by calling `client.OpenLTXFile` with the current byte position as the `offset` parameter. The underlying storage backends support this via HTTP Range requests. The reader retries up to `resumableReaderMaxRetries = 3` times before propagating the error.

Partial reads are returned to the caller without an error, which causes callers using `io.ReadFull` to loop back and request the remaining bytes—at which point the reconnect fires.

## Interface Dependency

`LTXFileOpener` is a single-method interface isolating the `OpenLTXFile` call. This allows `NewResumableReader` to accept a test double rather than a concrete `ReplicaClient`, making the reconnect logic independently testable without a real storage backend.