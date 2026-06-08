---
{
  "title": "Integration Test Helper Infrastructure",
  "summary": "Core test scaffolding for Litestream integration tests. Manages the full lifecycle of a test database and Litestream process including creation, population, load generation, replica verification, restore, and integrity validation.",
  "concepts": [
    "TestDB",
    "RequireBinaries",
    "litestream binary",
    "process management",
    "SIGTERM",
    "WAL mode",
    "Restore",
    "Validate",
    "WaitForReplicaFiles",
    "WaitForSnapshots",
    "log inspection",
    "config generation",
    "S3 access point",
    "integrity_check"
  ],
  "categories": [
    "testing",
    "integration",
    "infrastructure",
    "test"
  ],
  "source_docs": [
    "aae5fdaa706caf7a"
  ],
  "backlinks": null,
  "word_count": 378,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestDB` is the central struct for integration tests. It holds the database path, replica path and URL, config file path, and a reference to a running `litestream` process. Most integration tests create a `TestDB` via `SetupTestDB`, drive it through a scenario, and let deferred `Cleanup` handle teardown.

## Binary Management

`getBinaryPath` locates the `litestream` binary relative to the test directory, adding `.exe` on Windows. `RequireBinaries` calls this for both `litestream` and `litefs` (if needed) and skips the test if they are absent. This is necessary because integration tests cannot build the binary themselves — they test the already-built artifact.

## Database Lifecycle

`Create` opens a SQLite database and runs `PRAGMA journal_mode=WAL` to enable WAL mode, which Litestream requires. `CreateWithPageSize` does the same with a custom page size. `PopulateWithOptions` fills the database to a target size using configurable row and batch sizes.

## Litestream Process Management

`StartLitestream` writes a minimal YAML config file for the database+replica pair, then launches the `litestream replicate` subcommand as a child process. `StartLitestreamWithConfig` accepts a pre-built config path for tests that need custom settings (e.g., directory watching).

`StopLitestream` sends `SIGTERM` and waits for the process to exit. The function captures stdout+stderr via `configureCmdIO` and returns them for inspection.

## Verification

`Restore` runs `litestream restore` against the replica to produce a restored database. `Validate` opens both the source and restored databases and compares row counts on every table. `QuickValidate` runs SQLite's `PRAGMA integrity_check` on the restored database — faster than a full comparison but does not catch data divergence.

`WaitForReplicaFiles` polls until the replica directory contains at least `minFiles` LTX files. This is the synchronization point that lets tests know replication has actually started before they proceed.

`WaitForSnapshots` polls until a snapshot file appears in the replica, confirming the compaction pipeline has run at least once.

## Log Inspection

`GetLitestreamLog` returns the captured stderr from the Litestream process. `CheckForErrors` scans it for `level=ERROR` lines, distinguishing genuine errors from benign warnings.

## S3 Config Helper

`WriteS3AccessPointConfig` generates a config file for S3 access point tests, abstracting the credential and endpoint fields so individual tests don't repeat them.

## Known Gaps

- `PrintTestSummary` logs timing and file count information but does not export it as structured data, making automated analysis of soak test results difficult.