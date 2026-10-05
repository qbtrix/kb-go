---
{
  "title": "SQLite WAL Frame Reader with Checksum Verification",
  "summary": "WALReader is Litestream's streaming parser for SQLite Write-Ahead Log files that validates salt and checksum integrity on every frame as it reads. It exposes both sequential frame access and bulk page-map construction used during replica restore and compaction.",
  "concepts": [
    "WALReader",
    "SQLite WAL",
    "WAL frame",
    "checksum",
    "salt rotation",
    "PageMap",
    "FrameSaltsUntil",
    "WALChecksum",
    "PrevFrameMismatchError",
    "io.ReaderAt",
    "litestream",
    "replication",
    "LTX",
    "checkpoint"
  ],
  "categories": [
    "storage",
    "sqlite",
    "litestream",
    "parsing"
  ],
  "source_docs": [
    "065ea5548d2d1a67"
  ],
  "backlinks": null,
  "word_count": 698,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

SQLite's WAL format is the primary source of truth for database changes in Litestream's replication pipeline. The WAL file is a sequence of frames, each containing a database page plus a small header with page number, commit size, salt pair, and running checksum. `WALReader` wraps an `io.ReaderAt` and provides a verified sequential reader over this format.

The choice of `io.ReaderAt` rather than `io.Reader` is intentional: it allows `FrameSaltsUntil` to scan non-sequentially by computing byte offsets directly, without the overhead of maintaining separate state for a seek-based reader.

## Construction

```go
func NewWALReader(rd io.ReaderAt, logger *slog.Logger) (*WALReader, error)
func NewWALReaderWithOffset(ctx context.Context, rd io.ReaderAt, offset int64, salt1, salt2 uint32, logger *slog.Logger) (*WALReader, error)
```

`NewWALReader` always reads the WAL header first. The header encodes page size, byte order, sequence number, and initial salt values. Parsing at construction time lets all subsequent frame reads skip header re-parsing and fail fast if the file is truncated or uses an unsupported WAL version.

`NewWALReaderWithOffset` is for mid-WAL positioning — used when Litestream already knows a safe restart point (e.g., after a checkpoint). The caller supplies the expected salts; if the frame at `offset` has different salts the reader returns a `PrevFrameMismatchError`. This prevents silently replaying frames from a different WAL generation after SQLite performs a salt rotation during checkpointing.

## Frame Reading

`ReadFrame(ctx, data)` reads one frame into the caller-supplied buffer. It enforces that `len(data)` matches the page size encoded in the WAL header — a mismatched buffer would produce garbled output with no obvious error at the caller. The method delegates to `readFrame` with `verifyChecksum = true`.

Checksum verification uses the running-checksum algorithm mandated by the SQLite WAL spec: each frame's header checksum is computed over both the frame header bytes and the page data, seeded from the previous frame's checksum. A mismatch means the frame was written by a different WAL instance (after a salt rotation) or is genuinely corrupt. When this happens, `ReadFrame` returns `PrevFrameMismatchError`, which Litestream uses to detect the boundary between two WAL segments rather than treating it as an I/O error.

Transaction boundaries are explicitly **not** enforced by this reader. Frames with a zero commit field are uncommitted; the caller decides whether to include them. This deliberate choice keeps `WALReader` single-purpose and lets Litestream's higher-level compaction code implement its own transaction policy.

## Page Map Construction

```go
func (r *WALReader) PageMap(ctx context.Context) (m map[uint32]int64, maxOffset int64, commit uint32, err error)
```

`PageMap` reads all available frames and returns a map from page number to the byte offset of that page's **latest committed version** in the WAL. This is the critical operation during restore: rather than replaying every frame sequentially, Litestream can jump to the highest-offset entry per page and read exactly one copy of each page.

The implementation uses a two-stage accumulation: `txMap` collects offsets for the current transaction, then copies them into the main map `m` only when a commit frame (`fcommit != 0`) is seen. Frames in an uncommitted tail are discarded. After the loop, pages with numbers exceeding the final commit size are pruned — this handles `VACUUM` shrinkage where the WAL contains updates to pages that no longer exist in the smaller post-vacuum database.

## Salt Scanning

`FrameSaltsUntil` scans WAL frames at fixed stride intervals (header size + page size) and collects all unique `(salt1, salt2)` pairs until it reaches a target pair. This is used when Litestream needs to enumerate all WAL generations present in a single WAL file, which can happen when multiple checkpoint cycles have occurred without truncation.

## Checksum Algorithm

```go
func WALChecksum(bo binary.ByteOrder, s0, s1 uint32, b []byte) (uint32, uint32)
```

The exported `WALChecksum` function implements SQLite's native checksum: an 8-byte-strided sum where each 8-byte unit updates both `s0` and `s1` in a cross-dependent manner. The `bo` parameter supports both big-endian (native-endian WAL format) and little-endian reads depending on the WAL header's magic number. The `assert` call enforces 8-byte alignment — an unaligned slice would produce an incorrect checksum that could pass silently.

## Known Gaps

No TODO or FIXME markers are present. The reader does not enforce that frames are monotonically increasing in page number or that transaction IDs advance, leaving those invariants to callers.