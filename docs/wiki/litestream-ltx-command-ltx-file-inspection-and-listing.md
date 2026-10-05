---
{
  "title": "Litestream `ltx` Command: LTX File Inspection and Listing",
  "summary": "`ltx.go` implements the `litestream ltx` subcommand, which lists LTX (Litestream Transaction) files for a database from either a config-specified replica or a direct replica URL. It supports filtering by compaction level and displays transaction ID ranges and timestamps.",
  "concepts": [
    "LTX files",
    "compaction levels",
    "replica URL",
    "litestream ltx command",
    "transaction ID",
    "level filtering",
    "config lookup",
    "tabwriter",
    "replication debugging",
    "levelVar"
  ],
  "categories": [
    "litestream",
    "CLI",
    "debugging",
    "tooling"
  ],
  "source_docs": [
    "01222b6fdea20393"
  ],
  "backlinks": null,
  "word_count": 331,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

LTX files are the unit of replication in Litestream. Being able to list them directly is essential for debugging replication gaps, verifying that compaction is working correctly, and understanding the state of a replica without performing a full restore. The `ltx` command bridges the gap between internal Litestream state and operator visibility.

## Two Access Modes

The command accepts either a database file path (looked up in the config) or a direct replica URL:

```
litestream ltx /var/db/app.db           # config-based lookup
litestream ltx s3://bucket/myapp.db     # direct URL
```

The direct URL mode (`litestream.IsURL`) bypasses config loading entirely, which is useful when the config is not available (e.g., in a recovery scenario where the daemon is not running).

## Level Filtering

`levelVar` is a custom `flag.Value` that accepts either a numeric level (0–9) or the string `"all"`. When `"all"` is specified, LTX files at every compaction level are listed. When a specific level is specified, only files at that level are shown. This lets operators inspect L0 (raw transactions) separately from L1+ (compacted ranges).

## Output Format

Output uses `text/tabwriter` with columns: `level`, `min_txid`, `max_txid`, `size`, `created_at`. The `created_at` field shows when the LTX file was written to the replica, which is useful for diagnosing replication lag by comparing it to the primary's last commit timestamp.

## Config vs. URL Initialization

When using a config path, the command calls `ReadConfigFile` then looks up the database by the path argument, constructing a `Replica` via `NewReplicaFromConfig`. When using a direct URL, it calls `NewReplicaFromConfig` with a synthetic `ReplicaConfig{URL: ...}` and initializes logging to stdout (since there is no daemon process to log to).

## Known Gaps

- The command lists LTX files but does not validate their contents (checksums, transaction continuity). The `validate` command in `litestream-test` provides that functionality but is a separate binary.
- There is no `--since` or `--until` flag to filter by timestamp, so operators looking at a large number of LTX files must grep or pipe the output.