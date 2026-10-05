---
{
  "title": "Litestream v0.3.x Backup Format Compatibility Layer",
  "summary": "Defines the types, path helpers, filename parsers, and interface contract needed to read backup data produced by Litestream v0.3.x. This compatibility layer enables v5 to restore databases that were replicated with the old format without requiring any migration of the stored files.",
  "concepts": [
    "v0.3.x compatibility",
    "backup format",
    "PosV3",
    "ReplicaClientV3",
    "WAL segments",
    "snapshots",
    "filename parsing",
    "generation ID",
    "LZ4",
    "litestream",
    "restore",
    "path helpers"
  ],
  "categories": [
    "litestream",
    "compatibility",
    "storage",
    "backup"
  ],
  "source_docs": [
    "e78859f7fb2fa0c2"
  ],
  "backlinks": null,
  "word_count": 493,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`v3.go` is a read-only bridge between the v5 codebase and the legacy v0.3.x storage layout. It exposes no write paths — its entire purpose is to let the restore command understand the old file structure well enough to reconstruct a database from it.

## Position Representation

`PosV3` captures a point in a v0.3.x backup stream:

```go
type PosV3 struct {
    Generation string // 16-char hex string
    Index      int    // WAL index
    Offset     int64  // byte offset within WAL segment
}
```

The `String()` method formats as `{generation}/{index:08x}:{offset:016x}`, which is stable for use in log messages and error reporting. `IsZero()` enables nil-safe comparisons without pointer indirection.

The v5 format uses a different position type (`Pos` with a `TXID`), so `PosV3` is kept entirely separate rather than aliased. This prevents accidental mixing of position values across format versions.

## Path Conventions

Three constants define the v0.3.x directory layout:

- `GenerationsDirV3 = "generations"` — root of all backup generations.
- `SnapshotsDirV3 = "snapshots"` — per-generation snapshot files.
- `WALDirV3 = "wal"` — per-generation WAL segment files.

The helper functions (`GenerationsPathV3`, `GenerationPathV3`, `SnapshotsPathV3`, `WALPathV3`, etc.) build paths by concatenating these constants with `path.Join`. Centralizing path construction prevents typos that would cause restore to look in the wrong directory and silently find no files.

## Filename Format and Parsing

v0.3.x filenames encode their position in hex:

- Snapshots: `{index:08x}.snapshot.lz4`
- WAL segments: `{index:08x}_{offset:08x}.wal.lz4`

`FormatSnapshotFilenameV3` and `FormatWALSegmentFilenameV3` produce these names. Their parse counterparts use compiled regexps to validate the full filename before extracting values, returning an error for any deviation. This strictness prevents a file named `00000001.snapshot` (missing `.lz4`) from being silently accepted and then failing to decompress.

`IsGenerationIDV3` validates that a directory name is exactly 16 lowercase hex characters — the v0.3.x generation ID format. This check gates iteration over the generations directory so that unrelated files or symlinks left by operators do not cause panics.

## ReplicaClientV3 Interface

```go
type ReplicaClientV3 interface {
    GenerationsV3(ctx context.Context) ([]string, error)
    SnapshotsV3(ctx context.Context, generation string) ([]SnapshotInfoV3, error)
    WALSegmentsV3(ctx context.Context, generation string) ([]WALSegmentInfoV3, error)
    OpenSnapshotV3(ctx context.Context, generation string, index int) (io.ReadCloser, error)
    OpenWALSegmentV3(ctx context.Context, generation string, index int, offset int64) (io.ReadCloser, error)
}
```

Storage backends that support v0.3.x restores implement this interface. The interface is separate from the v5 `ReplicaClient` interface, which prevents the v3 read path from accidentally being used for v5 writes. Implementations for the file system and S3 backends implement both interfaces independently.

## Known Gaps

- The LZ4 decompression of snapshot and WAL segment streams is not handled in this file; it is expected to be done by callers. If a backend implementation forgets to decompress, the SQLite restore will silently produce a corrupt database.
- `ParseWALSegmentFilenameV3` parses the offset as a 32-bit hex string (`%08x`) but stores it as `int64`. This means WAL segments larger than 4 GB would parse correctly but could not have been produced by v0.3.x (which had a 4 GB WAL limit), so the constraint is implicit rather than enforced.