---
{
  "title": "Restore Command: Database Recovery from Replica Backup",
  "summary": "Implements the `litestream restore` sub-command, which reconstructs a SQLite database from replica backup files. Supports point-in-time recovery by timestamp or transaction ID, follow mode for streaming continuous restores, and post-restore integrity checking.",
  "concepts": [
    "RestoreCommand",
    "point-in-time recovery",
    "replica URL",
    "config-based restore",
    "txid",
    "timestamp",
    "follow mode",
    "integrity check",
    "PRAGMA quick_check",
    "if-db-not-exists",
    "if-replica-exists",
    "CalcRestoreTarget",
    "ErrTxNotAvailable"
  ],
  "categories": [
    "cli",
    "litestream",
    "recovery",
    "replication"
  ],
  "source_docs": [
    "53cef85cc5c65a74"
  ],
  "backlinks": null,
  "word_count": 486,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`RestoreCommand` recovers a SQLite database from its replica backups. It handles two source types: a direct replica URL (e.g., `s3://bucket/db`) or a database path that is looked up in the config file to find its associated replicas.

## Input Modes

`litestream.IsURL()` determines which path is taken:

- **Replica URL** — `loadFromURL()` creates a replica from the URL and validates the target path. The `-o` flag (output path) is required in this mode because the URL does not imply a local database path.
- **Database path + config** — `loadFromConfig()` reads the config file, finds the matching DBConfig, and selects the first configured replica. An output path defaults to the database's configured path.

The two modes are mutually exclusive with `-config` — combining a replica URL argument with `-config` returns an error, preventing ambiguous configuration.

## Point-in-Time Recovery Options

- **`-txid`** — restore to a specific transaction ID. Uses `txidVar` for hex parsing.
- **`-timestamp`** — restore to the state at a specific UTC time in RFC3339 format. Parsed strictly; an invalid format returns an error naming the expected format.
- If neither is specified, the most recent available state is used.

`CalcRestoreTarget()` is called during source loading to confirm that at least one backup file covers the requested point in time. This fails early before committing to the restore, which prevents a situation where the restore starts writing a partial database and then fails midway.

## Follow Mode

`-f` (follow mode) keeps the restore running after reaching the latest available backup, polling for new LTX files as they arrive. This is useful for:

- Continuous replication testing (watch the target stay in sync with the source)
- Read-replica setups where the restored database is continuously updated

In follow mode, signal handling is set up so Ctrl+C cancels the context cleanly, stopping the polling loop without leaving a partial database on disk.

`-follow-interval` controls the polling frequency, defaulting to 1 second.

## Integrity Checking

`-integrity-check` accepts three values:
- `none` (default) — skip checking
- `quick` — runs SQLite's `PRAGMA quick_check`
- `full` — runs `PRAGMA integrity_check`

Post-restore integrity checking exists because a restore involves replaying transaction files; a corrupted LTX file or an incomplete transmission could produce a database that opens without error but contains logically inconsistent data. Running `quick_check` immediately after restore surfaces these problems before the database is put into service.

## Idempotency Flags

- **`-if-db-not-exists`** — skip the restore entirely if the output file already exists. Safe for use in initialization scripts that run on every boot.
- **`-if-replica-exists`** — exit successfully even if no matching backup is found. Combined with `-if-db-not-exists`, this makes the restore command safe to run idempotently in environments where the backup may not yet exist.

## Known Gaps

No progress reporting during the restore — for large databases the command is silent until completion, making it hard to tell if it is hung or making progress.