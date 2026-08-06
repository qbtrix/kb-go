---
{
  "title": "Test Suite for Core Litestream Package — Checksum, Path Helpers, and Error Types",
  "summary": "Tests the fundamental correctness of the litestream package's checksum algorithm against a known WAL byte sequence, the path helper functions, and the LTXError hint generation for all error categories.",
  "concepts": [
    "SQLite checksum",
    "WAL checksum",
    "LTXError",
    "error hint",
    "path helpers",
    "LTXFilePath",
    "golden test",
    "hex encoding",
    "errors.Is",
    "errors.Unwrap"
  ],
  "categories": [
    "testing",
    "core",
    "litestream",
    "test"
  ],
  "source_docs": [
    "b874c22097fc1684"
  ],
  "backlinks": null,
  "word_count": 253,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file validates the core package's stateless utilities and error construction. Because these helpers are used by every subsystem, regressions here would propagate broadly.

## Checksum Test

`TestChecksum` verifies the SQLite checksum implementation against a hex-encoded golden byte string representing a real WAL header, frame header, and frame data. The test exercises the `OnePass` scenario—computing a running checksum over the entire WAL frame in a single call—and checks that the result matches a known-good value extracted from a real SQLite WAL file. This is the most rigorous form of checksum test: if the algorithm is off by even one bit ordering, the golden value will not match.

`MustDecodeHexString` is a test helper that calls `hex.DecodeString` and fatals on parse failure, keeping test setup clean.

## Path Helper Tests

`TestLTXDir`, `TestLTXLevelDir`, and `LTXFilePath` verify that path construction is deterministic and correctly encodes level integers and TXID values into the expected directory hierarchy. These tests catch accidental changes to path separators or zero-padding width that would break compatibility with existing replica storage.

## LTXError Tests

`TestNewLTXError` and `TestLTXErrorHints` verify that:
- The `Error()` string includes the operation, path, and underlying error message.
- `Unwrap()` returns the original error (enabling `errors.Is` and `errors.As`).
- The `Hint` field is set to the appropriate human-readable recovery suggestion for each error category (`os.ErrNotExist`, `ErrLTXCorrupted`, `ErrChecksumMismatch`, and the generic case).

The hint tests are important because the hints are the primary user-facing diagnostic tool. A wrong or missing hint would leave operators without guidance during data-recovery scenarios.