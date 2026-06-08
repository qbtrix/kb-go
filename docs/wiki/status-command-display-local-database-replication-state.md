---
{
  "title": "Status Command: Display Local Database Replication State",
  "summary": "Implements the `litestream status` sub-command, which reads the config file and displays a tabular summary of each database's local replication state including WAL size, the highest local transaction ID, and an overall status label. Operates offline without requiring a running daemon.",
  "concepts": [
    "StatusCommand",
    "tabwriter",
    "DBStatus",
    "MaxLTX",
    "WAL size",
    "go-humanize",
    "local TXID",
    "status labels",
    "not initialized",
    "offline status check"
  ],
  "categories": [
    "cli",
    "litestream",
    "monitoring"
  ],
  "source_docs": [
    "490ad9ba73c64dfa"
  ],
  "backlinks": null,
  "word_count": 362,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`StatusCommand` provides a quick snapshot of litestream's local knowledge about each configured database. Unlike the MCP `litestream_status` tool, this command reads the filesystem directly rather than querying a daemon — it works even when no daemon is running.

## Output Format

The command uses `text/tabwriter` to produce aligned columns:

```
database        status          local txid      wal size
/data/app.db    ok              0000000000000042  1.2 MB
/data/logs.db   not initialized -               0 B
```

The `go-humanize` library converts WAL file sizes to human-readable byte strings (`1.2 MB`, `512 kB`), preventing the raw byte count that would be hard to read for large WAL files.

## Status Labels

`getDBStatus()` inspects the local filesystem and returns one of four labels:

- **`ok`** — the database file exists, the LTX directory has at least one file, and `MaxLTX()` returned a valid TXID
- **`not initialized`** — the database file exists but no local LTX files have been written yet (fresh database with no syncs)
- **`no database`** — the database file does not exist at the configured path
- **`error`** — the database exists and the LTX directory exists, but `MaxLTX()` returned an error (e.g., corrupted file)

The `unknown` initial value is never returned — every code path sets one of the four labels. The `unknown` default is a safety net for future code additions.

## Filtering

If a positional argument is provided (`litestream status /data/app.db`), only that database's row is printed. The filter compares `db.Path()` exactly, so the path must match the configured path precisely. This can be confusing if one uses a relative path and the config stores an absolute path — the row will be silently omitted.

## WAL Size Reporting

The WAL file size is read from the `-wal` file (SQLite's write-ahead log). If the WAL does not exist, the size is reported as `0 B`. The WAL size is informational — a large WAL indicates a checkpoint is overdue, which can increase restore time if a backup is needed.

## Known Gaps

Replica TXID and sync lag are not shown. To see whether replicas are caught up, the user must check logs or use the MCP tools. The usage message acknowledges this limitation explicitly.