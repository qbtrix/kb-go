---
{
  "title": "VFS Compaction and Snapshot Unit Tests",
  "summary": "Tests the Litestream VFS compaction pipeline — from L0 LTX files through multi-level compaction and full snapshot creation — and verifies that the default four-level compaction configuration is correctly structured and passes validation. Requires the `vfs` build tag.",
  "concepts": [
    "VFS compaction",
    "LTX files",
    "L0 L1 L2 compaction levels",
    "Compactor",
    "snapshot",
    "TXID range",
    "DefaultCompactionLevels",
    "build tags",
    "vfs build tag",
    "litestream",
    "SQLite VFS",
    "compaction validation"
  ],
  "categories": [
    "testing",
    "litestream",
    "storage",
    "compaction",
    "test"
  ],
  "source_docs": [
    "d42d09fa7afdee31"
  ],
  "backlinks": null,
  "word_count": 546,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`vfs_compaction_test.go` (build tag `vfs`) exercises the compaction and snapshot subsystems of the Litestream VFS. Compaction is critical for long-running deployments: without it, the number of L0 LTX files grows unbounded, making startup restore progressively slower. These tests confirm that the `Compactor` correctly merges L0 files into higher levels and that the resulting merged file covers the expected TXID range.

## TestVFSFile_Compact

`ManualCompact` pre-creates three L0 LTX files with TXIDs 1, 2, and 3 using `createTestLTXFile`, then calls `compactor.Compact(ctx, 1)` to merge them into a single L1 file. The test asserts:

- The returned `info.Level` is 1 (compacted to L1, not left at L0).
- `info.MinTXID == 1` and `info.MaxTXID == 3` (all three files were consumed).

This catches a class of bugs where the compactor stops early (e.g., skips the last file) or produces a file with the wrong TXID range metadata — both of which would cause subsequent restore to miss transactions.

## TestVFSFile_Snapshot

`MultiLevelCompaction` tests the full L0 → L1 → L2 chain:

1. Compact three L0 files to L1.
2. Compact the resulting L1 file to L2.
3. List L2 files via `client.LTXFiles` and assert exactly one file exists.

The single-file assertion is important: if the L1-to-L2 compaction produced multiple L2 files, it would indicate that the compactor split the output incorrectly. The test also logs `minTXID` and `maxTXID` at each level, which aids debugging when the assertion fails.

## TestDefaultCompactionLevels

Verifies the `DefaultCompactionLevels` slice that ships with the VFS:

- L0: no interval (files are written as they arrive, no automatic merging).
- L1: 30-second merge interval.
- L2: 5-minute merge interval.
- L3: 1-hour merge interval.

The test calls `levels.Validate()` to confirm the slice passes the built-in consistency checks (e.g., intervals must be strictly increasing). This prevents a future change to the defaults from producing an invalid configuration that only fails at runtime.

## TestVFS_CompactionConfig

`DefaultConfig` verifies that a newly constructed `VFS` has compaction disabled by default (`CompactionEnabled == false`). This is a safety default: enabling compaction without understanding the retention implications could delete L0 files before they have been replicated to all consumers. Operators must explicitly opt in.

A second sub-test (not shown in the AST excerpt) likely verifies that setting `CompactionEnabled = true` and assigning custom `CompactionLevels` is reflected in the VFS before any files are opened.

## Build Tag

The `//go:build vfs` tag means these tests only run when `-tags vfs` is passed to `go test`. This is deliberate: the VFS tests depend on CGo (via `psanford/sqlite3vfs`) and require a SQLite shared library, which is not available in all CI environments. The tag isolates this dependency.

## Known Gaps

- `createTestLTXFile` is a helper defined elsewhere in the test package. Its implementation is not shown, so it is unclear whether it produces valid LTX binary content or a stub. If it produces stubs, the compaction tests may pass without actually exercising the LTX merge logic.
- The tests do not verify that L0 files are deleted after compaction — only that the L1/L2 output exists. A compactor that creates higher-level files but leaves L0 files in place would pass these tests but waste storage.
- There are no tests for compaction with overlapping TXID ranges, which could occur if two concurrent writers both produce L0 files.