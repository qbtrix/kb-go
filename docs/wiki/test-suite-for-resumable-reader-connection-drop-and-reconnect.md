---
{
  "title": "Test Suite for Resumable Reader — Connection Drop and Reconnect",
  "summary": "Tests the ResumableReader's three behavioral scenarios: normal pass-through reads, transparent reconnect after a non-EOF connection error, and transparent reconnect after a premature EOF from an idle-connection timeout.",
  "concepts": [
    "ResumableReader",
    "testLTXFileOpener",
    "errorAfterN",
    "premature EOF",
    "reconnect",
    "OpenLTXFile",
    "call count verification",
    "max retries",
    "unknown size",
    "connection drop simulation"
  ],
  "categories": [
    "testing",
    "io",
    "resilience",
    "litestream",
    "test"
  ],
  "source_docs": [
    "fc6f5b5e42b48199"
  ],
  "backlinks": null,
  "word_count": 299,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file directly exercises `ResumableReader` using injectable test doubles, without any real network or storage backend. The tests are in the `internal` package (not `internal_test`) so they can access `resumableReaderMaxRetries` and the unexported constructor details.

## Test Infrastructure

`testLTXFileOpener` is a simple struct with a function field `OpenLTXFileFunc` that implements `LTXFileOpener`. Each test case wires a different lambda to simulate different backend behaviors:

- Return a full reader immediately (normal case).
- Return a reader that errors after N bytes, then on the second call return a reader starting from the offset (reconnect case).
- Return a reader with only the first N bytes, producing premature EOF, then reconnect (idle connection case).

`errorAfterN` is a reader that emits data normally for the first `n` bytes, then returns the configured error. It tracks byte position precisely (not read-count) so it works correctly regardless of how large or small `Read` calls are.

`newTestResumableReader` creates a `ResumableReader` with the initial stream already opened from offset 0, which is the normal startup state (the first `OpenLTXFile` was called by the caller before wrapping).

## Test Cases

- **NormalRead**: A healthy stream is passed through byte-for-byte with no reconnect calls.
- **ReconnectOnError**: After 5 bytes of an 11-byte stream, a `"connection reset"` error fires. The reader reconnects from byte 5 and delivers the rest. `callCount` verifies exactly 2 `OpenLTXFile` calls.
- **ReconnectOnPrematureEOF**: A server returns only the first 5 bytes then clean EOF. The reader detects `offset < size` and reconnects from byte 5. Again verified with `callCount == 2`.
- **MaxRetriesExceeded**: A reader that always fails causes `ResumableReader` to exhaust all 3 retries and propagate the error to the caller.
- **UnknownSize**: When `size == 0`, a premature EOF is treated as a legitimate end-of-stream and passed through without reconnect.