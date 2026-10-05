---
{
  "title": "Test Suite for Configuration Parsing, Validation, and Replica Factories",
  "summary": "Comprehensive integration tests for the main.go configuration engine, covering YAML parsing, per-backend replica construction, validation rules, directory scanning, and database discovery. These tests catch regressions in the surface area that most directly affects user-facing behavior: config files, CLI arguments, and replica URL formats.",
  "concepts": [
    "Config.Validate",
    "ReadConfigFile",
    "NewDBFromConfig",
    "NewReplicaFromConfig",
    "DirectoryMonitor",
    "ParseByteSize",
    "IsSQLiteDatabase",
    "S3 access-point",
    "SFTP replica",
    "age encryption",
    "directory scanning",
    "replica URL",
    "compaction validation",
    "table-driven tests"
  ],
  "categories": [
    "testing",
    "configuration",
    "litestream",
    "cli",
    "test"
  ],
  "source_docs": [
    "fe750ce029bf835f"
  ],
  "backlinks": null,
  "word_count": 501,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file exercises the configuration and factory functions defined in `main.go`. It is in package `main_test`, an external test package, which means it tests only the exported API and exercises the same interface a real caller would use.

## Config File Parsing

`TestOpenConfigFile` and `TestReadConfigFile` verify that YAML files are read correctly, environment variables are expanded when expected, and SQLite connection string prefixes (`sqlite:` or `sqlite:///`) are stripped before the path is stored. The connection-string stripping prevents a common copy-paste mistake where users configure litestream with a DSN rather than a bare file path.

## Replica Construction

Each storage backend gets its own test:

- `TestNewFileReplicaFromConfig` — local filesystem, including path normalization
- `TestNewS3ReplicaFromConfig` — S3 bucket, region, path-style access, access-point URLs, and part-size/concurrency options
- `TestNewGSReplicaFromConfig` — Google Cloud Storage
- `TestNewSFTPReplicaFromConfig` — SFTP with host, user, and key path
- `TestNewReplicaFromConfig_AgeEncryption` — verifies that age encryption is explicitly rejected with a clear error rather than silently ignored

`TestParseReplicaURL_AccessPoint` confirms that S3 access-point URLs (which use a different hostname format) parse correctly and don't lose the access-point endpoint during normalization.

## Config Validation

Several tests exercise `Config.Validate()`:

- **Snapshot intervals** — snapshot interval must align with the highest compaction level's interval
- **Validation interval** — must be positive
- **L0 retention** — must be a multiple of the L0 compaction interval; fractions would cause files to be deleted mid-interval
- **Sync intervals** — per-replica sync intervals have minimum values to prevent polling storms
- **Compaction levels** — levels must be in order, intervals must be set for non-zero levels

`TestConfig_DefaultValues` confirms that `DefaultConfig()` produces sensible values so users who omit optional fields get reasonable behavior.

## Directory Database Discovery

The `TestNewDBsFromDirectoryConfig_*` suite covers the directory scanning logic:

- **Unique paths** — each discovered database gets a unique meta path to prevent state cross-contamination
- **Meta path per database** — confirms isolation between databases in the same directory
- **Subdirectory preservation** — relative paths within the directory are preserved in replica URLs
- **Duplicate filenames** — two `db.sqlite` files in different subdirectories produce distinct replica URL suffixes
- **S3 URL suffix** — verifies per-database suffixes are appended correctly
- **Special characters** — filenames with spaces, parentheses, and similar characters are sanitized for URL use
- **Empty base path** — scanning a directory with no databases returns no configs without error
- **Deprecated `replicas` array** — the old config format is handled for backward compatibility

`TestDirectoryMonitor_DetectsDatabaseLifecycle` and `TestDirectoryMonitor_RecursiveDetectsNestedDatabases` verify that the runtime directory watcher picks up databases created after startup and handles nested directory structures.

## Utility Coverage

- `TestParseByteSize` and `TestParseByteSizeOverflow` — IEC unit parsing and int64 overflow rejection
- `TestFindSQLiteDatabases` and `TestIsSQLiteDatabase` — magic byte detection
- `TestX509FallbackRoots` — confirms x509 fallback roots are present, guarding against TLS failures in minimal environments
- `TestStripSQLitePrefix` — normalizes DSN-style paths
- `TestGlobalDefaults` — validates that the global default config matches documented defaults

## Known Gaps

No TODOs in this file. The age encryption rejection (`TestNewReplicaFromConfig_AgeEncryption`) documents an unimplemented feature.