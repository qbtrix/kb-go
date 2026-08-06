---
{
  "title": "Reset Command: Clear Local LTX State for Database Recovery",
  "summary": "Implements the `litestream reset` sub-command, which deletes the local LTX files for a database so that the next replication sync produces a fresh full snapshot. Intended as a recovery tool when local state becomes corrupted or out of sync with remote replicas.",
  "concepts": [
    "ResetCommand",
    "LTX directory",
    "local state",
    "meta path",
    "DB.ResetLocalState",
    "recovery",
    "absolute path",
    "config-based meta path",
    "LTX corruption",
    "full snapshot trigger"
  ],
  "categories": [
    "cli",
    "litestream",
    "recovery"
  ],
  "source_docs": [
    "5cbd88f8beac99a8"
  ],
  "backlinks": null,
  "word_count": 397,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`ResetCommand` clears litestream's local metadata directory for a specific database. It does not modify the SQLite database file itself — only the `LTXDir()` (the directory containing locally-cached LTX transaction files) is removed.

## When Reset Is Needed

Litestream maintains a local cache of LTX files alongside the live database. In normal operation these files are produced incrementally as transactions occur. Several failure scenarios can leave this local state inconsistent:

- The host ran out of disk space mid-write, leaving a truncated LTX file
- The meta directory was partially deleted by an operator
- A bug or format change caused the local files to be unreadable
- The database was restored from backup but the LTX directory still references an older transaction range

In these cases, litestream may fail to start replication or produce incorrect backups. `reset` provides a clean escape hatch: remove the local state and let the next `sync` produce a full snapshot from the current database state.

## Path Resolution

The command converts relative database paths to absolute before doing any filesystem operations. This is necessary because the meta path is derived from the database path — a relative path would produce a relative meta path that resolves differently depending on the working directory at the time of the call. Absolute paths make the operation deterministic regardless of the current directory.

## Config Integration

If a `-config` flag is provided, the command reads the config file and looks up the database's `DBConfig` to use the configured meta path. Without a config, it falls back to `litestream.NewDB(dbPath)` which uses the default meta path convention (`.<basename>-litestream/` in the same directory as the database).

This two-path logic ensures the reset targets the same directory that the running daemon would use, even when a custom `meta-path` is configured.

## Safety Check

Before removing anything, the command verifies the database file exists (`os.Stat(dbPath)`). If the database file is missing, the reset is rejected. This prevents silently deleting LTX state for a database that has already been moved or deleted, which could confuse a subsequent restore.

If the meta directory does not exist (no local state), the command reports this and exits successfully — nothing to reset is not an error.

## Known Gaps

No dry-run mode. The command prints what it will delete but does not offer a `-dry-run` flag to preview the operation without executing it.