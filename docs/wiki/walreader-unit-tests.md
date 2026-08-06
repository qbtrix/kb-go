---
{
  "title": "WALReader Unit Tests",
  "summary": "Unit tests for Litestream's WALReader that verify correct frame parsing, error handling for malformed inputs, and salt enumeration across multiple WAL generations. Tests use golden WAL files from testdata to confirm byte-level correctness of checksum verification and offset tracking.",
  "concepts": [
    "WALReader",
    "SQLite WAL",
    "frame parsing",
    "checksum",
    "salt",
    "FrameSaltsUntil",
    "testdata",
    "io.EOF",
    "partial frame",
    "offset tracking",
    "litestream",
    "golden files"
  ],
  "categories": [
    "testing",
    "sqlite",
    "litestream",
    "parsing",
    "test"
  ],
  "source_docs": [
    "1ba2f222e5d35ca7"
  ],
  "backlinks": null,
  "word_count": 605,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The WALReader is a critical parsing component — incorrect frame offsets or missed checksum failures would cause Litestream to replicate corrupted data silently. These tests pin the parser to known-good WAL fixtures and systematically enumerate every failure mode the parser must handle.

## Happy Path: TestWALReader OK

The `OK` sub-test reads a pre-built WAL from `testdata/wal-reader/ok/wal` and walks through three frames in sequence:

1. **First frame** — page 1, non-commit (`commit == 0`). Verifies `pgno`, `commit`, raw page bytes against the expected offset in the file, and `Offset()` return value.
2. **Second frame** — page 2, end-of-transaction (`commit == 2`). Verifies the commit field reflects the final database page count.
3. **Third frame** — page 2 again, in a second transaction. Verifies Litestream correctly advances the frame counter across transaction boundaries.

Checking `r.Offset()` at each step ensures the reader maintains correct byte-position bookkeeping — callers use offsets to seek into WAL segments, so an off-by-one here would cascade into range-read failures during restore.

Page bytes are compared directly against known slices of the raw WAL file (`b[56:4152]`, `b[4176:8272]`), confirming that the header-skip arithmetic is correct. WAL frames have a 24-byte file header followed by per-frame 24-byte headers, so frame data starts at byte 56 (24 + 32-byte frame header … the test pins the exact layout).

## Error Cases

**`UnsupportedVersion`** — Constructs a synthetic header with WAL version field set to 1 and verifies that `NewWALReader` returns an error with the exact message `"unsupported wal version: 1"`. Without this check, the reader would parse version-1 frame headers using version-2 field offsets and produce nonsense.

**`ErrBufferSize`** — Calls `ReadFrame` with a 512-byte buffer on a WAL with 4096-byte pages and expects a descriptive error. The buffer size check prevents a silent short-read where the caller receives partial page data without realizing it.

**`ErrPartialFrameHeader`** — Truncates the WAL after the file header but before the first frame header completes (retains only 40 bytes). Expects `io.EOF`. This tests that a mid-flight truncation during replication doesn't panic or return garbage.

**`ErrFrameHeaderOnly`** — Includes a complete frame header (56 bytes) but no page data. Expects `io.EOF`. SQLite guarantees frames are written atomically, but a crash could leave a partial frame on disk; Litestream must handle this gracefully.

**`ErrPartialFrameData`** — Truncates to 1000 bytes (enough for header + partial page). Expects `io.EOF`. This exercises the partial-page read path distinct from the partial-header path.

## Salt Enumeration: TestWALReader_FrameSaltsUntil

The `OK` sub-test loads a fixture WAL containing three salt generations (representing three checkpoint cycles without truncation) and calls `FrameSaltsUntil` with a zero terminator `[2]uint32{0, 0}`. It then asserts:

- Exactly 3 unique salt pairs are found
- All three specific `(salt1, salt2)` pairs are present by value

The zero terminator is chosen because `FrameSaltsUntil` stops when it encounters the target salt or exhausts the file. Using zero means it will always scan to end-of-file in this fixture, collecting all generations.

This test is important because Litestream uses salt enumeration to decide which LTX segments are still valid after a WAL reuse — stale segments with salts not present in the current WAL can be safely pruned.

## Test Data Dependency

Both functions depend on golden files in `testdata/wal-reader/`. These files are binary SQLite WAL fixtures with known content. If the WAL format were to change, the tests would fail with offset mismatches rather than silently passing on wrong data, making them effective regression guards.

## Known Gaps

There are no tests for `PageMap` directly in this file — that coverage presumably lives elsewhere. There are also no tests for `NewWALReaderWithOffset` or `PrevFrameMismatchError`, leaving the salt-mismatch detection path without dedicated coverage here.