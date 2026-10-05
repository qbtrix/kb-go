---
{
  "title": "Directory Watcher Test Helpers",
  "summary": "Shared helper infrastructure for directory watcher integration tests. Provides test database creation, replica polling, concurrent database population, and error filtering utilities that make the directory watcher tests readable and reusable.",
  "concepts": [
    "directory watcher",
    "DirWatchTestDB",
    "WaitForDatabaseInReplica",
    "polling",
    "CreateFakeDatabase",
    "SQLite validation",
    "concurrent creation",
    "continuous writes",
    "error filtering",
    "test helpers",
    "integration"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "eb04bf58e40edc70"
  ],
  "backlinks": null,
  "word_count": 359,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`DirWatchTestDB` extends `TestDB` with directory-specific fields (`DirPath`, `Pattern`, `Recursive`) and a `CreateDirectoryWatchConfig` method that generates a YAML config file pointing Litestream at a directory rather than a specific database path.

## Test Environment Setup

`SetupDirectoryWatchTest` creates a temp directory structure, instantiates a `DirWatchTestDB`, and returns it ready for the test to create databases inside. Centralizing setup here avoids 20-line boilerplate in every test function.

## Database Creation Helpers

`CreateDatabaseInDir` creates a SQLite database at an optional subdirectory within the watched directory. The `subDir` parameter lets tests verify recursive watching by nesting databases multiple levels deep.

`CreateDatabaseWithData` creates a database and inserts a specified number of rows. Without data, some replication checks fail because Litestream only creates LTX files when there are actual transactions.

`CreateFakeDatabase` writes a file with `.db` extension but non-SQLite content. This is used by `TestDirectoryWatcherNonSQLiteRejection` to verify that Litestream validates database headers before replicating rather than blindly replicating any file.

## Replica Polling

`WaitForDatabaseInReplica` polls the replica directory until a corresponding LTX file appears for the given database path, with a configurable timeout. Polling is necessary because directory watcher detection is event-driven and asynchronous — the test cannot know precisely when Litestream will pick up a new file.

`VerifyDatabaseRemoved` does the inverse: polls until no new LTX files appear for a database, confirming it has been de-registered. The helper must wait rather than check immediately because cleanup is also asynchronous.

## Concurrency Helpers

`CreateMultipleDatabasesConcurrently` uses goroutines to create many databases simultaneously, producing the race condition that `TestDirectoryWatcherRapidConcurrentCreation` needs.

`StartContinuousWrites` launches a goroutine that inserts rows at a specified rate until the context is cancelled. It returns the goroutine's done channel and a row count pointer so tests can assert final counts.

## Error Filtering

`CheckForCriticalErrors` reads Litestream's log output and filters out known benign errors (e.g., "database is locked" transient messages during WAL growth), returning only genuinely unexpected errors. Without this, tests would fail on noise from normal SQLite locking behavior.

## Known Gaps

- `getRelativeDBPath` converts an absolute database path to a replica-relative path for lookup — this relies on the directory structure being predictable, which may break if the replica layout changes.