---
{
  "title": "Directory Watcher Integration Tests",
  "summary": "Integration tests for Litestream's directory watching feature, covering database lifecycle detection, recursive scanning, glob pattern matching, non-SQLite file rejection, active connection handling, restart recovery, rename operations, and concurrent write load.",
  "concepts": [
    "directory watcher",
    "auto-register",
    "auto-deregister",
    "glob pattern",
    "recursive scan",
    "rename",
    "TOCTOU race",
    "non-SQLite rejection",
    "restart recovery",
    "active connections",
    "concurrent creation",
    "pattern matching"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "18a52a1eb6a868c1"
  ],
  "backlinks": null,
  "word_count": 406,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

These tests validate the directory watcher — the feature that lets Litestream monitor a directory for new SQLite databases rather than requiring each database to be individually configured. All tests use the `integration` build tag and run against a real Litestream binary.

## Basic Lifecycle

`TestDirectoryWatcherBasicLifecycle` starts Litestream pointed at an empty directory, creates databases while it runs, verifies they are detected and replicated, then deletes them and verifies cleanup. The test confirms the three fundamental operations: auto-register on create, replicate, auto-deregister on delete.

## Race Conditions

`TestDirectoryWatcherRapidConcurrentCreation` uses `CreateMultipleDatabasesConcurrently` to create dozens of databases simultaneously. The test guards against a TOCTOU race where the watcher detects a file, starts setup, but the file disappears before the watcher finishes initializing the DB object.

## Recursive Mode

`TestDirectoryWatcherRecursiveMode` creates databases in nested subdirectories and asserts all are detected. Non-recursive mode is also tested — databases in subdirectories should be ignored when `Recursive: false`.

## Pattern Matching

`TestDirectoryWatcherPatternMatching` uses glob patterns like `*.db` and `app-*.sqlite` and verifies that only matching filenames are replicated. Non-matching files in the same directory should be ignored even if they are valid SQLite databases.

## Non-SQLite Rejection

`TestDirectoryWatcherNonSQLiteRejection` uses `CreateFakeDatabase` to place a non-SQLite file in the watched directory and asserts Litestream does not attempt to replicate it. Attempting to open a non-SQLite file as a database would produce corrupt LTX output.

## Active Connections

`TestDirectoryWatcherActiveConnections` holds an open SQLite connection to a database while Litestream detects and replicates it. This guards against lock errors or stalled replication when the watcher tries to read header information from a database that another process has open.

## Restart Behavior

`TestDirectoryWatcherRestartBehavior` stops and restarts Litestream while databases exist in the watched directory. On restart, Litestream must re-detect existing databases and resume replication from the correct position without re-syncing data that was already replicated.

## Rename Operations

`TestDirectoryWatcherRenameOperations` renames database files within the watched directory. SQLite databases are sometimes renamed during migrations or schema upgrades; the watcher must handle this as a deregister+register pair.

## Load with Writes

`TestDirectoryWatcherLoadWithWrites` uses `StartContinuousWrites` while new databases are being added to the directory. The combination of ongoing writes and new database detection exercises scheduling fairness in the watcher's goroutine pool.

## Known Gaps

- No test covers symlinks to database files within the watched directory.
- Cross-volume moves (rename from a different filesystem) are not tested — on Linux these produce a delete+create rather than a rename event.