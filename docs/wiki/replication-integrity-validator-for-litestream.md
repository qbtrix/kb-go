---
{
  "title": "Replication Integrity Validator for Litestream",
  "summary": "ValidateCommand restores a Litestream replica and runs configurable integrity checks against the restored database — quick sanity, SQLite integrity check, MD5 checksum comparison, row-count validation, and LTX file continuity. It is the final gate in the `litestream-test` pipeline.",
  "concepts": [
    "replication validation",
    "integrity check",
    "quick_check",
    "MD5 checksum",
    "LTX continuity",
    "row count comparison",
    "Litestream restore",
    "ValidationResult",
    "CI gate",
    "SQLite PRAGMA"
  ],
  "categories": [
    "litestream",
    "testing",
    "validation",
    "integrity"
  ],
  "source_docs": [
    "e60ce14e89afa8de"
  ],
  "backlinks": null,
  "word_count": 421,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Replication without validation is incomplete. A replica that streams LTX files without corruption errors might still produce a database that fails SQLite's internal consistency check or differs from the source by a few rows. `ValidateCommand` closes that gap by running multiple independent checks and reporting each result with pass/fail and duration.

## Check Types

### quick-check
`performQuickCheck` connects to the restored database and runs `PRAGMA quick_check`. This verifies page-level integrity (B-tree structure, free list consistency) without a full table scan. It is fast enough to run after every restore.

### integrity-check
`performIntegrityCheck` runs the full `PRAGMA integrity_check`, which additionally validates that all index entries match their table rows. Slower but catches index corruption that `quick_check` misses.

### checksum
`performChecksumCheck` computes an MD5 hash of both the source database file and the restored database file and compares them. This catches byte-level divergence but has an important caveat: SQLite WAL mode databases may have unflushed data in the WAL that is not reflected in the main file. The validator works around this by checkpointing the source before hashing.

### data-validation
`performDataValidation` compares row counts per table between source and restored database. This is more meaningful than a file checksum when the source has been checkpointed at a different time than the restore — row counts should be monotonically consistent with the LTX sequence.

### ltx-continuity
`validateLTXContinuity` checks that the LTX files in the replica form an unbroken sequence: `maxTXID` of file N equals `minTXID - 1` of file N+1. A gap means at least one transaction was lost during replication. This check calls the `litestream` CLI as a subprocess via `os/exec` to list LTX files, then parses the output.

## Result Reporting

All checks populate a `[]ValidationResult` slice. `reportResults` prints each result with its check type, pass/fail status, duration, and error message if any. The command exits non-zero if any check failed, making it suitable as a CI gate.

## Restore Step

`performRestore` calls the `litestream restore` CLI as a subprocess, passing the replica URL and output path. Running restore as a subprocess rather than as a library call means validate tests the same binary that operators would use, catching integration issues that a direct library call might miss.

## Known Gaps

- The `ltx-continuity` check parses CLI output rather than using the Litestream library directly, making it fragile to output format changes.
- MD5 is used for checksums. While MD5 collisions are theoretically possible, the practical risk for database integrity checking is negligible — but SHA-256 would be more principled.