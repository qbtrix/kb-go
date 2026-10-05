---
{
  "title": "Hex Dump Utility for Binary Debugging",
  "summary": "Provides a classic hex dump formatter that renders arbitrary byte slices as a human-readable 16-bytes-per-row display with hex values and printable-ASCII sidebars. Identical consecutive rows are collapsed to a single `***` marker to make large zero-padded regions readable.",
  "concepts": [
    "hex dump",
    "binary debugging",
    "duplicate row suppression",
    "printable ASCII",
    "WAL frame inspection",
    "LTX debugging",
    "bytes.Buffer",
    "xxd format"
  ],
  "categories": [
    "debugging",
    "utilities",
    "litestream"
  ],
  "source_docs": [
    "124cc6cc77df8fbc"
  ],
  "backlinks": null,
  "word_count": 300,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`Hexdump` is a diagnostic helper used to inspect LTX files, WAL frames, and other binary data during debugging. Its output matches the traditional Unix `xxd` / `hexdump -C` style that developers already know how to read.

## Format

Each output row covers 16 bytes and follows this layout:

```
00000000  41 42 43 44 45 46 47 48  49 4a 4b 4c 4d 4e 4f 50  |ABCDEFGHIJKLMNOP|
```

- The 8-digit hex offset of the first byte in the row.
- Two groups of 8 hex bytes separated by an extra space (to split 0–7 and 8–15).
- A pipe-delimited sidebar of printable ASCII characters; non-printable bytes (< 0x20 or > 0x7E) are shown as `.`.

## Duplicate Row Suppression

The formatter keeps a copy of the previous row. When the current row is byte-identical to the previous one—and is neither the first row nor the last partial row—it writes a single `***` line and skips all subsequent duplicates until the pattern breaks. The `dupWritten` boolean ensures the `***` appears only once per run of duplicates.

This is essential for binary debugging: SQLite WAL frames and database pages often contain large swaths of zeroed padding. Without suppression, a 4 KB zero-filled page would print 256 identical rows, making the output impossible to scan.

## `toChar` Helper

`toChar` maps a byte to its printable ASCII representation or `.`. The range `[32, 126]` is the standard printable ASCII window—exactly what the sidebar needs.

## Usage Context

This function is internal to the `litestream/internal` package. It is not part of the public API and is intended for use in log output, error messages with binary context, and debugging sessions. It allocates a `bytes.Buffer` and grows it row by row, which is acceptable for diagnostic use where performance is secondary to readability.