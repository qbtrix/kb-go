---
{
  "title": "Version Command: Print the Litestream Binary Version",
  "summary": "A minimal sub-command that prints the value of the package-level `Version` variable and exits. Used by operators and scripts to confirm the installed binary version and by the MCP server's `litestream_version` tool.",
  "concepts": [
    "VersionCommand",
    "Version variable",
    "ldflags",
    "build-time version",
    "sub-command dispatch",
    "MCP version tool",
    "flag.FlagSet",
    "ContinueOnError"
  ],
  "categories": [
    "cli",
    "litestream"
  ],
  "source_docs": [
    "52a78e152d367212"
  ],
  "backlinks": null,
  "word_count": 255,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`VersionCommand` is the simplest command in the litestream CLI. It accepts no arguments beyond standard flag parsing and prints the `Version` package variable to stdout.

## Why a Separate Command

Having `version` as a proper sub-command rather than a top-level flag (`--version`) follows the litestream command dispatch pattern where every operation is a named sub-command. It also allows the MCP server to call `litestream version` as a subprocess and capture its output cleanly, without needing to parse flag output or handle the special-case behavior that `--version` flags sometimes exhibit (exiting with a non-zero code, writing to stderr, etc.).

## Version Variable

The `Version` variable is defined elsewhere in the package (typically set at build time via `-ldflags "-X main.Version=v1.2.3"`). Printing it in a dedicated command makes it easy for:
- Operators to confirm which binary is installed
- Monitoring scripts to compare against expected versions
- The MCP `litestream_info` tool to include version in its status report

## Flag Parsing

The command still creates a `flag.FlagSet` with `ContinueOnError` and calls `Parse()` even though it has no flags. This ensures that passing unexpected flags like `litestream version --help` produces a standard usage message rather than a panic or ignored argument.

## Known Gaps

No structured output format (JSON) for machine parsing. A script that wants to compare versions must parse the plain text string. No test file for this command — the behavior is trivial enough that it is tested implicitly through the MCP integration and any end-to-end test that calls the version command.