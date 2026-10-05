---
{
  "title": "Test Suite for Replicate Command Flag Parsing",
  "summary": "Tests the `replicate` sub-command's argument parsing, focusing on mutual-exclusion rules for one-shot flags, the flag-positioning guard (issue #245), and log-level propagation. These tests are unit-level — they verify ParseFlags() behavior without starting a real daemon.",
  "concepts": [
    "ReplicateCommand",
    "ParseFlags",
    "one-shot flags",
    "force-snapshot",
    "enforce-retention",
    "flag positioning",
    "mutual exclusion",
    "log-level",
    "CLI argument parsing",
    "issue #245"
  ],
  "categories": [
    "testing",
    "cli",
    "litestream",
    "test"
  ],
  "source_docs": [
    "3908695294fac501"
  ],
  "backlinks": null,
  "word_count": 334,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file tests `ReplicateCommand.ParseFlags()` in isolation. No store, server, or SQLite file is created — the tests confirm that the command rejects invalid flag combinations before any I/O begins.

## One-Shot Flag Rules

`TestReplicateCommand_ParseFlags_OnceFlags` verifies the mutual-exclusion constraints:

- `-once` alone is valid
- `-once -force-snapshot` is valid
- `-once -enforce-retention` is valid
- `-once -force-snapshot -enforce-retention` is valid
- `-force-snapshot` without `-once` → error: `"cannot specify -force-snapshot flag without -once"`
- `-enforce-retention` without `-once` → error: `"cannot specify -enforce-retention flag without -once"`
- `-once -exec ...` → error: `"cannot specify -once flag with -exec"`

The `-once` dependency exists because `-force-snapshot` and `-enforce-retention` are designed for scheduled maintenance runs, not continuous daemon operation. Allowing them without `-once` would cause the daemon to snapshot or delete files on every sync cycle, which could overwhelm storage or violate retention policies.

## Flag Positioning Guard

`TestReplicateCommand_ParseFlags_FlagPositioning` covers a subtle parsing issue (referenced as issue #245): when a user writes `litestream replicate db.sqlite s3://bucket -exec echo test`, Go's flag parser stops at `db.sqlite` (the first non-flag argument) and silently ignores `-exec`. The command detects flags-after-positional-args by inspecting remaining arguments for strings that start with `-`, and returns a clear error.

Subtests confirm:
- `-exec` after positional args → error with flag name in message
- `-exec` before positional args → success
- `-config` after positional args → error
- Multiple flags in correct position → success
- Only database path provided (no replica URL) → the "must specify at least one replica URL" error, not the flag-positioning error

## Log Level Tests

`TestReplicateCommand_ParseFlags_LogLevel` verifies that `-log-level` is accepted and stored in the config. Log level is applied both to the structured logger and, when running in CLI mode (no config file), sets the `LOG_LEVEL` environment variable so the level persists across any child process invocations.

## Known Gaps

No tests for the config-file mode of ParseFlags (where no positional arguments are provided and a YAML file is read). Config file behavior is covered in `main_test.go` instead.