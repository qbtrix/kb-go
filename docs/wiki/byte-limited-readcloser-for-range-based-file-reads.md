---
{
  "title": "Byte-Limited ReadCloser for Range-Based File Reads",
  "summary": "Implements a size-capped io.ReadCloser by combining the standard library's io.LimitedReader semantics with a Close method, filling a gap in the Go standard library where io.LimitReader returns an io.Reader but not an io.Closer.",
  "concepts": [
    "io.LimitedReader",
    "io.ReadCloser",
    "byte range read",
    "LimitReadCloser",
    "Close propagation",
    "LTX file",
    "partial read",
    "standard library gap"
  ],
  "categories": [
    "utilities",
    "io",
    "litestream"
  ],
  "source_docs": [
    "c02d22d6d9369a28"
  ],
  "backlinks": null,
  "word_count": 240,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`LimitReadCloser` exists because the Go standard library's `io.LimitReader` returns an `io.Reader`, not an `io.ReadCloser`. When litestream opens an LTX file from a remote backend and wants to serve only a specific byte range to the caller, it needs to both cap reads *and* propagate `Close()` to release the underlying network connection or file descriptor. The standard `io.LimitedReader` struct has no `Close` method.

## Implementation

`LimitedReadCloser` wraps an `io.ReadCloser` (`R`) and a byte counter (`N`). The `Read` method mirrors `io.LimitedReader` exactly:

1. If `N <= 0`, return `io.EOF` immediately.
2. Clamp the read slice to at most `N` bytes.
3. Delegate to the underlying reader.
4. Subtract bytes read from `N`.

`Close` delegates directly to `R.Close()`. This ensures that when the caller is done (or the limit is reached and the caller discards the reader), the underlying resource is cleaned up.

## Usage Context

`LimitReadCloser` is called in `file.ReplicaClient.OpenLTXFile` when a `size > 0` parameter is provided, enabling byte-range reads within a local file. Similar usage is expected in S3, GCS, and other backends that support partial object reads. The `size` parameter corresponds to a known LTX page or segment size so callers can avoid reading past the end of a logical chunk within a larger file object.

## Known Gaps

The doc comment says "Copied from the io package"—the logic is indeed identical to `io.LimitedReader`. If the stdlib ever adds `Close` to `io.LimitedReader`, this wrapper could be removed.