---
{
  "title": "Internal IO Utilities, File Helpers, and Prometheus Metrics",
  "summary": "Provides the shared foundational utilities used throughout the litestream codebase: composable IO wrappers (ReadCloser, LZ4ReadCloser, ReadCounter), cross-platform file creation with ownership preservation, a MkdirAll variant that matches uid/gid, and the global Prometheus metric vectors.",
  "concepts": [
    "ReadCloser",
    "LZ4ReadCloser",
    "ReadCounter",
    "io.Reader composition",
    "file ownership",
    "Chown",
    "MkdirAll",
    "Prometheus metrics",
    "promauto",
    "LevelTrace",
    "slog",
    "LZ4 decompression"
  ],
  "categories": [
    "utilities",
    "infrastructure",
    "litestream"
  ],
  "source_docs": [
    "cdc81a6f2d6fa173"
  ],
  "backlinks": null,
  "word_count": 361,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is the internal utility layer that all other litestream packages depend on. It groups together small, reusable primitives that do not belong in any one subsystem.

## IO Wrappers

### ReadCloser
`ReadCloser` combines an `io.Reader` and a separate `io.Closer` into a single `io.ReadCloser`. The motivation is that Go's standard library often provides `*os.File` as both reader and closer, but storage backends return a reader body and a separate HTTP response closer. `NewReadCloser` lets callers compose these without writing a bespoke struct each time. `Close` is careful to close the reader first (if it also implements `io.Closer`) before calling the outer closer, propagating the first error it encounters.

### LZ4ReadCloser
`LZ4ReadCloser` wraps a `lz4.Reader` with the underlying `io.Closer` of the compressed source. The `lz4.Reader` itself does not own a closer, so closing the decompressor would not close the source network connection or file. `LZ4ReadCloser.Close` calls only the underlying closer, correctly releasing the underlying resource.

### ReadCounter
`ReadCounter` transparently wraps any `io.Reader` and accumulates bytes read. This is used for tracking transfer sizes for Prometheus metrics without requiring callers to change their read logic.

## File Creation with Ownership Matching

`CreateFile` creates or truncates a file and sets its permission mode to match a reference `os.FileInfo`. If the info is non-nil, it also calls `Chown` to match uid/gid. This matters when litestream runs as root but manages databases owned by another user—restored files must retain their original ownership, or the database owner cannot open them. The `Chown` error is intentionally ignored (`_ = f.Chown(...)`) because it fails gracefully on non-root processes.

`MkdirAll` is a customized `os.MkdirAll` that also propagates ownership on each created directory segment.

## Prometheus Metrics

The file declares global Prometheus counter and histogram vectors:
- `OperationTotalCounterVec` — counts operations by type (read, write, delete) and backend.
- `OperationBytesCounterVec` — tracks bytes transferred.
- `OperationDurationHistogramVec` — operation latency histograms.
- `OperationErrorCounterVec` — counts errors.
- `L0RetentionGaugeVec` — tracks L0 file retention state.

Declaring these at package level via `promauto.NewXxx` automatically registers them with the default Prometheus registry.

## Log Level

`LevelTrace = slog.LevelDebug - 4` defines a custom log level below Debug for extremely verbose trace logging.