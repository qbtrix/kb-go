---
{
  "title": "Backup Format Compatibility Integration Tests",
  "summary": "Integration tests verifying that backups created by the current Litestream version can be restored correctly, including format consistency, LTX file validation, cross-platform path handling, point-in-time accuracy, CLI binary restore compatibility, directory layout migration, and compaction compatibility.",
  "concepts": [
    "backup compatibility",
    "LTX format",
    "restore",
    "point-in-time restore",
    "CLI binary",
    "directory layout",
    "v0.3.x migration",
    "compaction compatibility",
    "cross-platform paths",
    "file validation",
    "createValidLTXData"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "92512bd1b9f8d4c9"
  ],
  "backlinks": null,
  "word_count": 424,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file guards against regressions that break restore compatibility — either because the backup format changed without a migration path, or because the restore logic makes assumptions that fail in certain environments. Tests are in the `integration_test` package and use both the Go API and the `litestream` CLI binary.

## Format Consistency

`TestRestore_FormatConsistency` creates a backup using the programmatic API and restores it. It primarily checks that LTX files written by the current version are readable and produce a valid SQLite database. This is the baseline test that all other compatibility tests build on.

## Multi-Sync Accumulation

`TestRestore_MultipleSyncs` runs dozens of sync cycles and verifies that restore succeeds after many LTX files accumulate. Without this, a bug in the file iterator that silently stops after N files would pass format tests but fail under normal usage.

## LTX File Validation

`TestRestore_LTXFileValidation` injects invalid LTX files (wrong magic, truncated header, corrupted checksum) into the replica path and asserts that restore returns an appropriate error rather than silently producing a corrupted database. `createValidLTXData` is a helper that generates minimal but spec-compliant LTX files for testing valid-invalid contrasts.

## Cross-Platform Paths

`TestRestore_CrossPlatformPaths` tests path styles that differ across operating systems — absolute paths, paths with `..` components, and paths with special characters. This guards against path-joining bugs that appear only on Windows or when the replica root is on a different filesystem mount point.

## Point-in-Time Accuracy

`TestRestore_PointInTimeAccuracy` inserts rows with known timestamps, requests a restore to a time between two inserts, and verifies the restored database contains exactly the rows that existed at that timestamp. The test guards against off-by-one errors in timestamp comparison where a restore might include one extra or one fewer transaction.

## CLI Restore Compatibility

`TestBinaryCompatibility_CLIRestore` creates a backup programmatically and restores it using the `litestream restore` CLI command via `os/exec`. This confirms that the Go library and the CLI binary agree on the file format — a gap that could exist if the CLI uses different parsing logic.

## Directory Layout Migration

`TestVersionMigration_DirectoryLayout` places backup files in the old v0.3.x directory layout and verifies that the current version auto-detects and restores from them.

## Compaction Compatibility

`TestCompaction_Compatibility` runs compaction to L2 and then restores, verifying that files compacted into higher levels can be read back correctly. This guards against a compaction format change breaking restores of already-compacted backups.

## Known Gaps

- Tests rely on the `litestream` binary being built and available at test time; there is no fallback if the binary is missing beyond `RequireBinaries` skipping the test.