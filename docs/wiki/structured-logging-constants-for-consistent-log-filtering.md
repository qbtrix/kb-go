---
{
  "title": "Structured Logging Constants for Consistent Log Filtering",
  "summary": "Defines the slog attribute key names and value constants used throughout litestream so that log consumers can filter, group, and correlate log lines by system, subsystem, and database path.",
  "concepts": [
    "slog",
    "structured logging",
    "log key constants",
    "LogKeySystem",
    "LogKeySubsystem",
    "LogKeyDB",
    "log filtering",
    "log routing",
    "compactor",
    "WAL reader"
  ],
  "categories": [
    "logging",
    "utilities",
    "litestream"
  ],
  "source_docs": [
    "65bf3b1e8a77873e"
  ],
  "backlinks": null,
  "word_count": 201,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file is intentionally minimal: it defines only string constants. Its purpose is to give the entire litestream codebase a single authoritative source for structured log attribute names. Without this centralization, individual packages might spell keys differently (`"sys"` vs `"system"`, `"db"` vs `"database"`), making log filtering fragile.

## Log Key Constants

- `LogKeySystem = "system"` — the top-level component (store or server).
- `LogKeySubsystem = "subsystem"` — the sub-component within a system (compactor or WAL reader).
- `LogKeyDB = "db"` — the database file path being operated on.

These keys are passed as attributes to `slog.With(...)` calls when constructing per-component loggers, so every log line from the store subsystem automatically carries `system=store`.

## System and Subsystem Values

```go
LogSystemStore  = "store"
LogSystemServer = "server"

LogSubsystemCompactor = "compactor"
LogSubsystemWALReader = "wal-reader"
```

These constants prevent typos from causing silent log routing failures. In structured logging pipelines (Loki, Datadog, CloudWatch), a single misspelled key breaks a dashboard or alert rule.

## Usage Pattern

Typical usage in the codebase:

```go
logger := slog.Default().With(
    internal.LogKeySystem, litestream.LogSystemStore,
    internal.LogKeyDB, db.Path(),
)
```

This approach makes it trivial to add a new subsystem by adding a constant here rather than searching for every log call site.