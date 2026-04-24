---
{
  "title": "Test Suite for LTX Transaction ID and Compaction Level Parsing",
  "summary": "Validates the CLI flag types that wrap LTX transaction IDs and compaction level integers, ensuring proper hex formatting, boundary enforcement, and the special 'all' sentinel. These tests guard against user-visible command-line argument corruption that would silently replicate from the wrong position or wrong level.",
  "concepts": [
    "txidVar",
    "levelVar",
    "ltx.TXID",
    "compaction level",
    "hex parsing",
    "flag.Value",
    "SnapshotLevel",
    "levelAll",
    "CLI flag types",
    "table-driven tests"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "5b85be94121fdf39"
  ],
  "backlinks": null,
  "word_count": 448,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file covers two custom `flag.Value` implementations used throughout the litestream CLI: `txidVar` and `levelVar`. Both types adapt domain-specific litestream concepts into standard Go flag parsing, and the tests ensure the parsing round-trips correctly.

## txidVar: Transaction ID Parsing

`txidVar` wraps `ltx.TXID` (a `uint64`) and formats it as a zero-padded 16-character hex string. The format is strict: exactly 16 hex digits, case-insensitive. Tests cover:

- **Valid lowercase hex** — the typical format produced by litestream itself (`0000000000000002`)
- **Valid uppercase hex** — users may paste IDs from logs that use uppercase (`00000000000000FF`)
- **Too-short inputs** — rejected to prevent silently truncating a TXID
- **Too-long inputs** — rejected to prevent overflow and misidentification
- **Non-hex characters** — letters beyond `f` (like `g`) produce an error

The strict length requirement exists because TXID is the primary key for locating LTX files in storage. A silently truncated TXID would cause a restore or `ltx` inspection to seek from the wrong position, potentially skipping transactions or reading the wrong file.

The `String()` method always pads to 16 characters, ensuring the zero value (`0000000000000000`) and max value (`ffffffffffffffff`) both round-trip correctly. The max-value test is important because it validates `^uint64(0)` — the largest possible transaction — which is used internally as a sentinel.

## levelVar: Compaction Level Parsing

`levelVar` wraps a compaction level integer and adds two parsing behaviors beyond a plain integer:

1. **Range enforcement** — only values 0–9 are accepted. Level 9 is `SnapshotLevel`, the full-database snapshot tier.
2. **Special `all` token** — maps to `levelAll`, a sentinel that means "apply to every level." This is used by commands like `reset` and `ltx` to act on all compaction levels at once.

Tests confirm:

- Numeric levels 0–9 parse and round-trip correctly
- The `all` string parses to `levelAll` and serializes back as `"all"`
- Negative values are rejected — there is no level -1
- Values above 9 are rejected — level 10 would exceed the snapshot level boundary
- Non-numeric, non-`all` strings produce an error
- Empty string is rejected, preventing a silent zero default

The boundary at 9 is enforced because `SnapshotLevel = 9` is a reserved constant in the litestream package. Allowing level 10 as a compaction target would silently attempt an operation on a non-existent tier, producing confusing errors downstream.

## Testing Pattern

Both suites use the standard Go table-driven pattern with `t.Run()` subtests. Each case specifies an input string, an expected value, and whether an error is expected. The pattern ensures new edge cases can be added without restructuring, and failure messages include the input string for easy debugging.

## Known Gaps

No TODOs or known incomplete implementations found in this file.