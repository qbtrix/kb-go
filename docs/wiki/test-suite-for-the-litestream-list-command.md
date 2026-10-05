---
{
  "title": "Test Suite for the Litestream `list` Command",
  "summary": "list_test.go covers argument validation, timeout validation, connection error handling, and the successful list response for the `litestream list` subcommand, mirroring the structure of info_test.go.",
  "concepts": [
    "litestream list",
    "test coverage",
    "argument validation",
    "timeout validation",
    "Unix socket testing",
    "mock server",
    "shared test helpers",
    "testingutil",
    "connection error"
  ],
  "categories": [
    "testing",
    "litestream",
    "CLI",
    "unit",
    "test"
  ],
  "source_docs": [
    "13777296cee361d0"
  ],
  "backlinks": null,
  "word_count": 236,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The `list` and `info` commands share the same Unix socket HTTP transport pattern. Testing both independently ensures that changes to one command's argument parsing or output formatting do not inadvertently break the other via shared code.

## Test Coverage

The test structure mirrors `info_test.go`:

- **TooManyArguments**: passing a positional argument returns "too many arguments".
- **ConnectionError**: non-existent socket returns an error.
- **CustomTimeout**: `--timeout 1` is accepted and the timeout fires correctly.
- **InvalidTimeoutZero**: `--timeout 0` returns "timeout must be greater than 0".
- **InvalidTimeoutNegative**: `--timeout -1` returns the same error.

A successful path test creates a mock Unix socket server returning a JSON array of database entries and verifies the command prints the expected table output.

## Shared testSocketPath

Both `info_test.go` and `list_test.go` use `testSocketPath` from `info_test.go` (in the same package). The shared atomic counter ensures both test files can run in parallel without socket path collisions.

## testingutil Dependency

The test imports `litestream/internal/testingutil`, which provides helpers like `MustCloseSQLDB` and `NewDB`. These are shared across the test suite to avoid duplicating setup patterns, though for `list_test.go` the primary use is creating mock store state for the successful-path test.

## Known Gaps

- Like `info_test.go`, the mock server does not validate the requested URL path, so a bug that changes the endpoint from `/list` to `/databases` would not be caught.
- The `--json` output mode is not tested, leaving the raw JSON path untested.