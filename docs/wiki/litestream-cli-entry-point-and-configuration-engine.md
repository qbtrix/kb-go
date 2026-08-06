---
{
  "title": "Litestream CLI Entry Point and Configuration Engine",
  "summary": "The main entry point for the litestream binary, wiring together all sub-commands and providing the full configuration parsing, validation, and factory logic for databases and replicas. This file defines the Config struct hierarchy and translates YAML or CLI arguments into live DB and Replica objects for every supported storage backend.",
  "concepts": [
    "Main",
    "Config",
    "DBConfig",
    "ReplicaConfig",
    "CompactionLevel",
    "DirectoryMonitor",
    "ParseReplicaURL",
    "NewDBFromConfig",
    "NewReplicaFromConfig",
    "ReplicaClient",
    "ParseByteSize",
    "IsSQLiteDatabase",
    "log/slog",
    "YAML configuration",
    "x509 fallback roots",
    "MCP server",
    "environment variable expansion"
  ],
  "categories": [
    "cli",
    "configuration",
    "litestream",
    "replication"
  ],
  "source_docs": [
    "ed3ae7a5cd0302de"
  ],
  "backlinks": null,
  "word_count": 598,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is the core of the litestream command-line tool. It contains the `Main` struct that dispatches to sub-commands, the `Config` hierarchy that represents a parsed YAML configuration file, and the factory functions that construct live `DB` and `Replica` objects from configuration. It also handles global concerns like structured logging initialization, environment variable expansion, and x509 root certificate fallback.

## Main and Sub-command Dispatch

`Main.Run()` parses the first argument as a sub-command name and delegates to one of the registered command types: `replicate`, `restore`, `status`, `databases`, `sync`, `start`, `stop`, `register`, `unregister`, `reset`, `ltx`, `snapshot`, `version`. Unknown commands produce a usage error. This pattern keeps each command self-contained while the dispatcher handles common flag parsing.

## Config Hierarchy

`Config` is the top-level YAML-parsed struct. Key fields:

- **DBs** — list of `DBConfig` entries, each describing a SQLite path and its replicas
- **Levels** — compaction level definitions (default: levels 0–3 at 0s/30s/5m/1h)
- **Snapshot** — snapshot schedule configuration
- **Retention** — how long snapshot and L0 files are kept
- **Exec** — optional subprocess to run alongside replication
- **MCPAddr** — address for the MCP (Model Context Protocol) HTTP server
- **ShutdownSyncTimeout / ShutdownSyncInterval** — bounds for the shutdown sync retry loop

`Config.propagateGlobalSettings()` copies top-level compaction and retention settings down into each `DBConfig`, unless the DB has overrides. This prevents silent misconfigurations where a DB inherits a stale global default.

`Config.Validate()` enforces business rules: snapshot intervals must align with compaction intervals, L0 retention must be a multiple of the compaction interval, replica sync intervals must satisfy minimum values, and all compaction levels must pass `CompactionLevels.Validate()`.

## Replica and DB Factories

`NewDBFromConfig()` constructs a `litestream.DB` with the configured meta path, checkpoint settings, compaction levels, and retention parameters. If a DBConfig contains directory-based monitoring, `NewDBsFromDirectoryConfig()` scans the directory for SQLite files and expands a single directory config into multiple per-database configs.

`NewReplicaFromConfig()` dispatches on the URL scheme to build the appropriate `ReplicaClient`:

- `s3://` → S3 client (with optional access-point URL parsing)
- `gs://` → Google Cloud Storage
- `abs://` → Azure Blob Storage
- `sftp://` → SFTP
- `nats://` → NATS object store
- `oss://` → Alibaba Cloud OSS
- `webdav://` → WebDAV
- `file://` (or bare path) → local filesystem

The `ParseReplicaURL()` function normalizes both `file:///path` and plain `/path` forms to avoid user confusion.

## Directory Monitoring and Discovery

`NewDBsFromDirectoryConfig()` expands a directory path into individual `DBConfig` entries by scanning for SQLite files. It generates per-database replica URL suffixes using the file's relative path within the directory, so two databases with the same filename in different subdirectories never share a storage prefix. Special characters in filenames are sanitized before inclusion in remote URLs.

`DirectoryMonitor` watches a directory for new SQLite databases at runtime, enabling the `replicate` command to pick up databases created after startup without a restart.

## Supporting Utilities

- `ParseByteSize()` — parses human-readable sizes like `100MiB` or `2GiB` with IEC unit support and int64 overflow protection
- `IsSQLiteDatabase()` — checks the SQLite magic header bytes to distinguish real SQLite files from other content
- `ReadConfigFile()` — reads and optionally expands environment variables in YAML
- `initLog()` — configures `slog` with text or JSON format and a log level parsed from the config or `LOG_LEVEL` env var
- The `x509roots/fallback` blank import ensures TLS connections work even on systems with missing or outdated CA bundles, preventing replica upload failures in minimal container images

## Known Gaps

No TODO or FIXME markers found in the reviewed portions. The `age` encryption configuration is explicitly rejected with a clear error (`TestNewReplicaFromConfig_AgeEncryption`), indicating the feature was planned but not implemented.