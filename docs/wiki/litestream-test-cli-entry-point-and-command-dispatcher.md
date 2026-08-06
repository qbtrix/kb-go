---
{
  "title": "litestream-test CLI Entry Point and Command Dispatcher",
  "summary": "`main.go` for the `litestream-test` binary wires together the command dispatcher, I/O abstraction, and version reporting. It uses the `Main` struct pattern common in Go CLIs to make the binary testable by allowing stdin/stdout/stderr injection.",
  "concepts": [
    "CLI dispatcher",
    "Main struct pattern",
    "I/O injection",
    "slog",
    "version reporting",
    "subcommand routing",
    "litestream-test",
    "testability",
    "flag parsing",
    "Go binary"
  ],
  "categories": [
    "litestream",
    "CLI",
    "tooling"
  ],
  "source_docs": [
    "1aec89087aea47d1"
  ],
  "backlinks": null,
  "word_count": 290,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

`litestream-test` is a standalone binary separate from the main `litestream` daemon. It bundles the `populate`, `load`, `shrink`, and `validate` subcommands into a single tool for running replication integrity and performance tests without modifying the production binary.

## Main Struct Pattern

```go
type Main struct {
    Stdin  io.Reader
    Stdout io.Writer
    Stderr io.Writer
}
```

Exposing I/O streams as fields rather than hard-coding `os.Stdin` etc. makes the binary testable: tests can inject `bytes.Buffer` instances and capture output without spawning a subprocess. The `NewMain()` constructor wires the fields to the real OS streams for production use.

## Command Dispatch

`Main.Run` reads `args[0]` as the subcommand name and delegates to the appropriate command's `Run` method. Each command receives the remaining args slice, so flags are parsed per-subcommand rather than globally. This avoids flag pollution between commands and allows each subcommand to define its own flag set independently.

The dispatch also handles the `version` subcommand via `VersionCommand`, which prints the `Version` and `Commit` variables. These are intended to be set at link time with `-ldflags "-X main.Version=1.2.3 -X main.Commit=abc123"`, a standard Go release practice.

## Structured Logging

The `init()` function configures `slog` as the default logger. The log level is read from the `LITESTREAM_LOG_LEVEL` environment variable, defaulting to `INFO`. Using `slog` rather than `log` allows downstream tools to parse structured JSON log output when operators set the format to JSON.

## Known Gaps

- `Version` and `Commit` default to the string `"development"` and empty string respectively. Builds that do not set these at link time are indistinguishable from development builds, which can make debugging field issues harder.
- The binary has no `--help` flag at the top level; `help` must be a registered subcommand or the user must run a subcommand with `--help`.