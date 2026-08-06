---
{
  "title": "Litestream `databases` Command: List Configured Databases",
  "summary": "`databases.go` implements the `litestream databases` subcommand, which reads the Litestream config file and prints a tab-aligned table of every managed database path and its replica backend type. It is a diagnostic tool for operators to quickly verify what Litestream is managing.",
  "concepts": [
    "litestream databases command",
    "config file",
    "replica type",
    "tabwriter",
    "DefaultConfigPath",
    "registerConfigFlag",
    "no-expand-env",
    "operator tooling",
    "database listing"
  ],
  "categories": [
    "litestream",
    "CLI",
    "configuration",
    "tooling"
  ],
  "source_docs": [
    "64e740724b3bb2d6"
  ],
  "backlinks": null,
  "word_count": 291,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

A running Litestream daemon may manage dozens of databases from a single config file. Operators need a quick way to confirm that the config was parsed correctly and that each database is wired to the expected replica backend. The `databases` command provides this without requiring access to the daemon's runtime state.

## Config-First Design

Unlike `info` and `list` (which query the running daemon via Unix socket), `databases` reads the config file directly. This means it works even when the daemon is not running — useful during initial setup or when debugging a startup failure.

The `--config` flag (registered via `registerConfigFlag`) accepts an explicit path; if omitted, `DefaultConfigPath()` provides the platform-appropriate default (`/etc/litestream.yml` on Linux, `~/.litestream.yml` on macOS). The `--no-expand-env` flag disables environment variable substitution in the config, useful when the config contains literal `$VAR` strings that should not be expanded.

## Output Format

Output uses `text/tabwriter` with two-space padding, producing aligned columns:

```
path              replica
/var/db/app.db    s3
/var/db/audit.db  abs
```

Tab-aligned output is easier to scan than space-padded output when database paths have varying lengths.

## Replica Type Display

For each configured database, the command calls `db.Replica.Client.Type()`. This returns the short type string registered by the backend's `init()` function (`"s3"`, `"abs"`, `"file"`, etc.). The display shows the backend type rather than the full URL to avoid leaking credentials that may be embedded in the URL.

## Known Gaps

- The command constructs a `litestream.DB` object via `NewDBFromConfig` for each database, which may perform validation beyond config parsing. If a database path does not exist on disk, this may return an error that is not clearly attributed to the specific database in the output.
- There is no machine-readable output format (no `--json` flag), making the command harder to script.