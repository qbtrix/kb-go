---
{
  "title": "Test Suite: VFS Hydration End-to-End via SQLite CLI",
  "summary": "hydration_e2e_test.go verifies that the Litestream VFS loadable extension can be used directly from the `sqlite3` CLI, testing the `LITESTREAM_HYDRATION_ENABLED` and `LITESTREAM_HYDRATION_PATH` environment variables that control on-demand replica materialization.",
  "concepts": [
    "VFS extension",
    "SQLite CLI",
    "hydration",
    "loadable extension",
    "LITESTREAM_HYDRATION_ENABLED",
    "e2e testing",
    "CGO",
    "shared library",
    "environment variables",
    "platform constraints"
  ],
  "categories": [
    "testing",
    "litestream",
    "VFS",
    "integration",
    "test"
  ],
  "source_docs": [
    "1fe9051273a244aa"
  ],
  "backlinks": null,
  "word_count": 419,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The VFS extension can be loaded into any SQLite-speaking tool, not just Go programs. A common use case is ad-hoc querying of a replica via the `sqlite3` CLI. These tests verify that the extension behaves correctly when invoked through the CLI rather than through the Go library, catching integration gaps that library tests would miss.

## Build and Platform Constraints

`buildVFSExtension` compiles the VFS as a shared library (`.so` on Linux, `.dylib` on Darwin) using `go build -buildmode=c-shared`. This requires CGO and a C compiler, so the test skips on Windows and checks for the `sqlite3` binary in PATH before running.

```go
if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
    t.Skip("skipping: test only runs on darwin or linux")
}
if _, err := exec.LookPath("sqlite3"); err != nil {
    t.Skip("skipping: sqlite3 CLI not found in PATH")
}
```

These guards prevent CI failures on environments that legitimately cannot run these tests rather than treating every skip as a hidden failure.

## Hydration Environment Variables

`LITESTREAM_HYDRATION_ENABLED=1` tells the VFS extension to materialize a local copy of the replica on first access rather than reading every page over the network. `LITESTREAM_HYDRATION_PATH` specifies where to write the materialized file.

The test verifies four scenarios:
- **Normal hydration**: path + enabled, queries return correct data.
- **Temp file**: enabled but no path specified — VFS should use a temp file.
- **Disabled**: no env vars set — VFS should not create any local file.
- **Multiple queries**: hydration state persists across queries within one session.

## runSQLiteCLI Helper

`runSQLiteCLI` constructs a command that loads the extension and runs a `.load` meta-command followed by the SQL query. The extension path and environment variables are passed explicitly. Output is captured and returned as a string for assertion. The helper treats any non-zero exit code as a test failure with the stderr output included in the error message.

## Replica Setup

`setupTestReplica` creates a file replica with a known schema and row count. `setupTestReplicaWithMoreData` adds additional rows. Tests use these helpers to establish a baseline, then verify hydrated queries return the expected rows, confirming that the hydrated snapshot is consistent with the replica state at the time of hydration.

## Known Gaps

- `findProjectRoot` uses a heuristic (walk up from `runtime.Caller` path until a `go.mod` is found) that may break if the test is run from an unusual working directory or in a compiled binary without source.
- The tests only cover `darwin` and `linux`; Windows behavior of the loadable extension is completely untested.