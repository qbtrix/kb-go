---
{
  "title": "Litestream `list` Command: Runtime Database Inventory via Daemon",
  "summary": "`list.go` implements the `litestream list` subcommand, querying the running daemon's `/list` Unix socket endpoint to display all databases currently managed at runtime. Unlike `databases`, this reflects the daemon's live state rather than the config file.",
  "concepts": [
    "litestream list command",
    "Unix socket",
    "runtime database inventory",
    "lag reporting",
    "HTTP client",
    "daemon query",
    "tabwriter",
    "replica state",
    "control socket"
  ],
  "categories": [
    "litestream",
    "CLI",
    "monitoring",
    "tooling"
  ],
  "source_docs": [
    "14668cc74d474318"
  ],
  "backlinks": null,
  "word_count": 264,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The `databases` command reads the config file, which shows what the daemon should be managing. The `list` command shows what it is actually managing right now — after dynamic additions from `DirectoryMonitor`, after any databases that failed to open were skipped, and after any runtime changes. Operators use `list` to confirm that the daemon has picked up a newly created database file without restarting.

## Implementation

The implementation is structurally identical to `info.go`: a Unix socket HTTP client, a `--socket` flag, a `--timeout` flag, and a `--json` flag. The only differences are the endpoint path (`/list` instead of `/info`) and the output format (a table of database paths and replica states rather than daemon metadata).

## Output Format

Without `--json`, the response is parsed and printed as a tab-aligned table:
```
path               replica    lag
/var/db/app.db     s3         0ms
/var/db/audit.db   abs        150ms
```

The `lag` column shows how far behind each replica is from the latest committed transaction, giving operators an at-a-glance replication health view.

## Timeout Validation

Same `timeout <= 0` guard as `info.go`. Both commands share this validation because both use `http.Client.Timeout` where zero means "no timeout" — a surprising default that would cause the command to hang indefinitely if the daemon is unresponsive.

## Known Gaps

- The default socket path is the same hardcoded value as `info.go`. These two commands could share a common `controlClient` helper to avoid duplicating the socket connection logic, but currently they do not.
- There is no filtering by database path or replica type, so operators managing hundreds of databases must grep the output themselves.