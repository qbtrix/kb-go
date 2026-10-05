---
{
  "title": "Core Litestream Package — Constants, Path Helpers, and Error Types",
  "summary": "The root package file establishes the global constants, error sentinels, path conventions, the LTXError type with contextual recovery hints, and utility functions for SQLite WAL reading, temp file cleanup, and checksum computation.",
  "concepts": [
    "ErrLTXMissing",
    "ErrLTXCorrupted",
    "ErrChecksumMismatch",
    "LTXError",
    "recovery hint",
    "LTXFilePath",
    "MetaDirSuffix",
    "WAL header",
    "SQLite checksum",
    "removeTmpFiles",
    "checkpoint mode"
  ],
  "categories": [
    "core",
    "error handling",
    "litestream"
  ],
  "source_docs": [
    "6921ec5b6884abb6"
  ],
  "backlinks": null,
  "word_count": 315,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is the foundation of the `litestream` package. It does not contain large business logic, but it defines the shared vocabulary—error values, path formats, and helper functions—that every other file in the package references.

## Error Sentinels

- `ErrNoSnapshots` — returned during restore when no baseline snapshot exists yet.
- `ErrChecksumMismatch` — LTX file data does not match its recorded checksum.
- `ErrLTXCorrupted` — the LTX binary structure is malformed.
- `ErrLTXMissing` — an expected LTX file is absent from the replica.

## LTXError — Actionable Error Messages

`LTXError` wraps any of the above sentinels with structured context (operation name, file path, level, TXID range) and a `Hint` string. `NewLTXError` sets the `Hint` based on the underlying error type:

- Missing file: suggests running `litestream reset <db>` or deleting the meta directory.
- Corrupted file: suggests deleting the corrupted LTX and restarting.
- Checksum mismatch: suggests the replica may have been written by a different database version.

This matters operationally: binary protocol errors in a database tool are notoriously hard to diagnose. Embedding a recovery suggestion in the error message itself reduces the support burden significantly.

## Path Helpers

`LTXDir`, `LTXLevelDir`, and `LTXFilePath` generate canonical paths for the litestream metadata directory:

```
<db>.sqlite-litestream/ltx/<level>/<minTXID>-<maxTXID>.ltx
```

The `MetaDirSuffix = "-litestream"` constant makes the naming convention explicit.

## WAL Utilities

`readWALHeader` and `readWALFileAt` read from SQLite WAL files. `readWALFileAt` is explicitly documented as unsafe to use on the main database file because opening the database causes SQLite to register it, conflicting with litestream's own view.

## Checksum

`Checksum` computes SQLite's rolling checksum algorithm over a byte slice, taking a byte order and two seed values as input. This is needed when litestream validates WAL frame integrity before writing LTX files.

## removeTmpFiles

Walks the metadata directory and removes any `.tmp` files left by interrupted write operations. Called on startup to clean up after a crash.