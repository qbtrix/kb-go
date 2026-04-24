---
{
  "title": "Internal Test Suite for Replica Apply Logic and Database Integrity Checks",
  "summary": "White-box tests for the Replica's internal LTX application pipeline, covering gap-fill from compacted higher-level files, level-zero fallback to compaction, iterator close error propagation, checksum verification on close, and SQLite integrity check modes.",
  "concepts": [
    "applyNewLTXFiles",
    "gap-fill",
    "compacted file",
    "level fallback",
    "iterator close error",
    "checksum verification",
    "checkIntegrity",
    "PRAGMA integrity_check",
    "errorFileIterator",
    "white-box testing",
    "incremental LTX"
  ],
  "categories": [
    "testing",
    "restore",
    "core",
    "litestream",
    "test"
  ],
  "source_docs": [
    "2e9d820e1547f952"
  ],
  "backlinks": null,
  "word_count": 273,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file is in package `litestream` (not `litestream_test`), giving it access to unexported methods like `applyNewLTXFiles`, `applyLTXFile`, and `checkIntegrity`. These tests focus on the complex restore-time logic that cannot be exercised through the public API alone.

## Gap-Fill Tests

`TestReplica_ApplyNewLTXFiles_FillGapWithOverlappingCompactedFile` tests the scenario where level-0 files have a gap (no L0 file covers TXIDs 100–200) but a compacted level-1 file covers that range. The apply loop must detect the gap and fall back to the higher-level compacted file to fill it, then continue with the subsequent L0 file (TXID 201). This prevents restore failures when L0 files have been compacted away.

`TestReplica_ApplyNewLTXFiles_LevelZeroEmptyFallsBackToCompaction` tests a clean slate where L0 has no files but L1 has a compacted file. The apply loop should accept the L1 file rather than treating an empty L0 as an error.

## Error Propagation Tests

`TestReplica_ApplyNewLTXFiles_IteratorCloseError` uses `errorFileIterator`—an iterator whose `Close()` returns a non-nil error—to verify that close errors are propagated back to the caller rather than silently swallowed. This matters because storage backends may flush data on close; ignoring close errors could mean silently losing data.

`TestReplica_ApplyLTXFile_VerifiesChecksumOnClose` verifies that after applying an LTX file, the checksum is validated when the LTX reader is closed. A checksum mismatch detected at this point indicates data corruption during transfer.

## Integrity Check Tests

`TestCheckIntegrity_Quick_ValidDB`, `TestCheckIntegrity_Full_ValidDB`, `TestCheckIntegrity_None_Skips`, and `TestCheckIntegrity_CorruptDB` verify that `checkIntegrity` correctly passes valid databases, skips when mode is `"none"`, and returns an error for a deliberately corrupted SQLite file.

## Test Helpers

`mustBuildIncrementalLTX` constructs a syntactically valid incremental LTX file with a single page frame, enabling tests to simulate real LTX apply operations without a full litestream pipeline.