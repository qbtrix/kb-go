---
{
  "title": "Unit Tests for Litestream v0.3.x Format Helpers",
  "summary": "Comprehensive table-driven unit tests that validate every exported function in v3.go: position zero checks, string formatting, snapshot and WAL segment filename round-trips, path construction, and generation ID validation. The round-trip test is the key invariant: anything Format produces must be recoverable by Parse.",
  "concepts": [
    "unit tests",
    "table-driven tests",
    "round-trip testing",
    "filename parsing",
    "PosV3",
    "generation ID",
    "snapshot filename",
    "WAL segment filename",
    "hex encoding",
    "litestream v0.3.x",
    "path helpers"
  ],
  "categories": [
    "testing",
    "litestream",
    "compatibility",
    "backup",
    "test"
  ],
  "source_docs": [
    "440bcb20e6869ddd"
  ],
  "backlinks": null,
  "word_count": 480,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`v3_test.go` provides fine-grained coverage for the v0.3.x compatibility layer. Because these helpers are used exclusively during restore operations, a subtle bug (e.g., a filename that parses but reconstructs the wrong index) would cause a restore to silently apply the wrong WAL segment — data corruption with no error.

## PosV3 Tests

`TestPosV3_IsZero` verifies the zero-value contract: a default `PosV3{}` must report `IsZero() == true`, and any struct with a non-empty field must return `false`. This matters because callers use `IsZero()` to detect "no position" without pointer comparisons.

`TestPosV3_String` uses a table with two cases:

- Zero value → empty string (avoids confusing `"0/00000000:0000000000000000"` in logs).
- Non-zero value → `"0123456789abcdef/00000001:0000000000001000"` (offset 4096 = 0x1000).

The hex padding is load-bearing: restore code uses these strings in object storage paths, and mismatched padding would result in path lookups that find nothing.

## Snapshot Filename Tests

`TestFormatSnapshotFilenameV3` and `TestParseSnapshotFilenameV3` are paired. The format test confirms zero-padding to 8 hex digits. The parse test covers both valid and invalid inputs:

**Valid:** `"00000001.snapshot.lz4"` → index 1.

**Invalid cases include:**
- Missing `.lz4` extension.
- Wrong hex length (7 or 9 chars).
- Non-hex character (`g`).
- Uppercase extension (`.SNAPSHOT.lz4`).
- Extra suffix (`.lz4.bak`).

Each invalid case must return an error. The exhaustive invalid list prevents the parser from being accidentally relaxed (e.g., accepting 9-char hex after a refactor).

## WAL Segment Filename Tests

`TestFormatWALSegmentFilenameV3` and `TestParseWALSegmentFilenameV3` follow the same pattern. WAL segment filenames carry two positions (`index` and `offset`), so the invalid test set includes mismatches like:
- Single-component filename (missing `_offset`).
- Short or long hex in either component.
- Wrong extension (`.wal` missing `.lz4`, or `.WAL.lz4`).

## Generation ID Validation

`TestIsGenerationIDV3` checks that exactly 16 lowercase hex characters are accepted. It tests strings of length 15, 17, and 16 with an uppercase letter to confirm the regex anchors both ends.

## Path Construction

`TestPathsV3` calls the five path helpers with known inputs and verifies exact output strings. This prevents silent regressions if the directory constants are renamed.

## Round-Trip Test

`TestFormatParseRoundtrip` is the key invariant test:

```go
// For every (index, offset) pair:
got, _ := litestream.ParseWALSegmentFilenameV3(
    litestream.FormatWALSegmentFilenameV3(index, offset))
// got must equal (index, offset)
```

This confirms that Format and Parse are true inverses. Without this test, a change that pads offsets to 10 hex digits in Format but only reads 8 in Parse would pass all other tests but silently corrupt the round-trip.

## Known Gaps

- There are no fuzz tests. The parser handles a fixed regex but fuzz testing would catch edge cases like null bytes or very long strings that the regex might match unexpectedly.
- `TestPathsV3` does not test OS-specific path separators. On Windows, `path.Join` (not `filepath.Join`) is used in v3.go, so the paths would be Unix-style regardless of OS — this is correct for object storage keys but could confuse local file system operations.