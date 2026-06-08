---
{
  "title": "Test Suite for the Status Command",
  "summary": "Tests the `status` sub-command with a missing config, a valid config, and a path filter, verifying that the command handles all three cases without errors. Tests use temporary directories and inline YAML config files to avoid external dependencies.",
  "concepts": [
    "StatusCommand",
    "config file loading",
    "path filter",
    "temporary directory",
    "inline YAML",
    "tabwriter",
    "status display",
    "not initialized status"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "bd58662e150b4f7c"
  ],
  "backlinks": null,
  "word_count": 287,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file tests `StatusCommand.Run()` with three subtests that cover the main execution paths: config loading failure, successful display of all databases, and filtering by a specific database path.

## Test Cases

### NoConfig

Passes a non-existent config path. Expects the command to return an error. This confirms that a missing config is treated as a fatal error rather than silently displaying nothing.

### WithConfig

Creates a temporary SQLite file and a config YAML that references it with a local file replica. Calls the command and expects no error. The test does not assert on the output format — it only verifies that the command exits cleanly when given a valid, minimal config. The SQLite file is created as an empty file rather than a real SQLite database, which means the database will show `not initialized` status (no LTX files), but the command should still run without error.

### FilterByPath

Same setup as `WithConfig` but passes the database path as a positional argument. Expects no error. This confirms the filtering code path does not crash or return an error when the path matches a configured database.

## Limitations of These Tests

The tests verify command exit behavior but do not capture or assert on stdout output. If the tabwriter or humanize formatting breaks, these tests would not catch it. Similarly, the four status label paths (`ok`, `not initialized`, `no database`, `error`) are not individually exercised — a real database with LTX files would be needed to reach `ok` status.

## Known Gaps

No test for the case where a filter path does not match any configured database (expected: empty output, no error). No test for the replica TXID display (not yet implemented in the command itself).