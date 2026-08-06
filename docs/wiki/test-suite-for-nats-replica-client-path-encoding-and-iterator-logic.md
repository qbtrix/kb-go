---
{
  "title": "Test Suite for NATS Replica Client — Path Encoding and Iterator Logic",
  "summary": "Unit tests for the NATS replica client focusing on LTX path formatting and parsing, not-found error detection, the file iterator state machine, and client default configuration values.",
  "concepts": [
    "path encoding",
    "parseLTXPath",
    "ltxPath",
    "ltxFileIterator",
    "state machine",
    "isNotFoundError",
    "round-trip test",
    "NATS client defaults",
    "TXID hex encoding"
  ],
  "categories": [
    "testing",
    "replication",
    "litestream",
    "test"
  ],
  "source_docs": [
    "82cc2a063b3662a7"
  ],
  "backlinks": null,
  "word_count": 258,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file covers the pure-logic components of the NATS client that can be tested without a running NATS server. Integration tests requiring a live NATS connection are absent here, making this a fast, reliable unit-test suite.

## Path Round-Trip Tests

`TestReplicaClient_ltxPath` verifies that `ltxPath(level, minTXID, maxTXID)` produces the expected `ltx/<level>/<hex>-<hex>.ltx` format for three cases:
- Level 0 with small TXIDs.
- Level 1 with mid-range hex values.
- Level 255 with maximum TXID values (`0xffffffffffffffff`).

`TestReplicaClient_parseLTXPath` verifies the inverse: valid paths round-trip correctly, and invalid paths (wrong segment count, non-integer level, malformed TXID range) all return errors. This bidirectional coverage prevents regressions where one side of the encode/decode pair is changed without updating the other.

## Not-Found Error Classification

`TestReplicaClient_isNotFoundError` tests against a `mockNotFoundError` (which implements the not-found interface the NATS SDK uses) and a generic `mockOtherError`. The test ensures that `isNotFoundError` returns true only for the correct error type, preventing false positives that would mask real errors as missing files.

## Iterator State Machine

`TestLtxFileIterator` manually steps through a three-element `ltxFileIterator` and verifies:
- `Item()` returns nil before the first `Next()` call (initial state).
- Each `Next()` call advances and `Item()` returns the correct entry.
- `Next()` returns false after the last element.
- `Err()` returns nil throughout a successful iteration.

This matters because incorrect iterator state (e.g., returning items before `Next()` is called) would corrupt restore logic that depends on the iterator protocol.

## Default Configuration Test

`TestReplicaClientDefaults` creates a zero-value client and checks that `NewReplicaClient()` sets expected defaults for reconnect timing fields.