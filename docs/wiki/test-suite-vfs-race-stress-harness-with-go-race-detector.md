---
{
  "title": "Test Suite: VFS Race Stress Harness with Go Race Detector",
  "summary": "stress_test.go runs the VFS replica under concurrent write and read load with the Go race detector enabled, surfacing data races in the VFS page cache, poll loop, and connection management. The test is gated behind environment variables and build tags to avoid destabilizing standard CI.",
  "concepts": [
    "race detector",
    "stress test",
    "concurrent reads",
    "concurrent writes",
    "build tags",
    "LITESTREAM_ALLOW_RACE",
    "runtime.RaceEnabled",
    "page cache races",
    "VFS goroutines",
    "isBusyError"
  ],
  "categories": [
    "testing",
    "litestream",
    "VFS",
    "concurrency",
    "test"
  ],
  "source_docs": [
    "5ea52636e203ea8e"
  ],
  "backlinks": null,
  "word_count": 350,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The Go race detector can find concurrent memory access bugs that unit tests and even chaos tests cannot surface because they do not run enough goroutines simultaneously. The VFS layer manages shared state (page cache, LTX file handles, connection maps) across multiple goroutines. A race condition in any of these would cause silent data corruption in production.

## Build and Runtime Guards

```go
//go:build vfs && stress
```

The `stress` build tag ensures this test is never run accidentally. Two additional runtime guards:

```go
if os.Getenv("LITESTREAM_ALLOW_RACE") != "1" {
    t.Skip("...modernc.org/sqlite checkptr panics are still unresolved")
}
if !runtime.RaceEnabled() {
    t.Skip("requires go test -race")
}
```

The `LITESTREAM_ALLOW_RACE` guard documents a known issue: `modernc.org/sqlite` (the pure-Go SQLite driver) has unresolved `checkptr` panics under the race detector that are not caused by Litestream. Requiring this environment variable ensures operators know they are accepting those panics as expected noise.

`runtime.RaceEnabled()` returns true only when compiled with `-race`. This prevents the test from silently passing without the race detector active, which would defeat its purpose.

## Test Structure

1. A primary database is opened and seeded with 100 rows.
2. A VFS replica is opened with a 5ms poll interval.
3. Two goroutines run for 5 seconds:
   - **Writer**: inserts random rows into the primary, ignoring busy errors (expected under concurrent access).
   - **Reader**: runs SELECT queries against the VFS replica concurrently.
4. After 5 seconds, the test asserts no data races were detected (the race detector would have already panicked if any occurred).

## `isBusyError` Helper

The writer loop uses `isBusyError` to distinguish SQLite `SQLITE_BUSY` errors (expected, safe to ignore) from genuine write failures. Without this distinction, the test would fail spuriously when the VFS's background poller briefly holds a read lock that blocks the writer.

## Known Gaps

- The `modernc.org/sqlite checkptr` panics are documented as unresolved. This means the stress test is unreliable in practice and may not be run regularly, limiting its value as a regression signal.
- The 5-second duration is not configurable, unlike the soak test. Longer durations would increase the probability of surfacing rare races.