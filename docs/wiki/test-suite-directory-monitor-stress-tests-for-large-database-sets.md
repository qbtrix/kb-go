---
{
  "title": "Test Suite: Directory Monitor Stress Tests for Large Database Sets",
  "summary": "directory_watcher_stress_test.go tests DirectoryMonitor at scale — 100 to 2500 databases — verifying detection, dynamic creation, and concurrent write handling. Tests are gated behind the `stress` build tag and use progressive count arrays to characterize performance degradation.",
  "concepts": [
    "stress testing",
    "DirectoryMonitor",
    "fsnotify",
    "scalability",
    "database detection",
    "dynamic creation",
    "concurrent writes",
    "waitForDBCount",
    "inotify limits",
    "build tags"
  ],
  "categories": [
    "testing",
    "litestream",
    "stress",
    "filesystem",
    "test"
  ],
  "source_docs": [
    "4d118fedea21dfdf"
  ],
  "backlinks": null,
  "word_count": 330,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

A directory monitor that works for 10 databases may fail for 1000 due to fsnotify watch limits, goroutine contention in the store's internal map, or OS inotify descriptor exhaustion. The stress tests characterize the monitor's scalability by running at multiple scales and timing each.

## Build Tag

```go
//go:build stress
```

Stress tests are excluded from standard `go test` and CI runs. They require elevated OS limits (`ulimit -n`) for large database counts and take several minutes to complete.

## Test Scenarios

**`TestDirectoryWatcher_PreCreated`**: creates `count` databases before starting the monitor, then waits for all to be detected. This tests the initial scan path (`addInitialWatches` → `scanDirectory`) rather than the event-driven path. Timeout scales linearly with count (3 minutes base + 1 minute per 100 databases).

**`TestDirectoryWatcher_DynamicScaling`**: starts the monitor on an empty directory, then creates databases in batches using `createTestDatabasesBatch`. This tests the fsnotify event path under concurrent creation load, where rapid file creation may overwhelm the debounce queue.

**`TestDirectoryWatcher_ConcurrentWrites`**: creates databases and immediately starts writing to them concurrently, verifying that the monitor correctly handles the case where a database is written before Litestream has fully registered it.

## waitForDBCount

```go
func waitForDBCount(ctx context.Context, store *litestream.Store, expected int) error
```

Polls `store.DBs()` until the count matches `expected` or the context expires. The polling interval is not specified in the AST but is typically 100ms. This helper is the test's primary assertion mechanism.

## Database Count Array

```go
var dbCounts = []int{100, 250, 500, 1000, 2500}
```

Running the same test at multiple scales in a single binary lets operators produce a scaling curve without writing separate tests. The exponential progression catches O(n²) or worse behavior early.

## Known Gaps

- Tests do not check for goroutine leaks after `stopDirectoryMonitor`. A monitor that leaks goroutines would not be caught.
- The `startDirectoryMonitor` helper creates a file-backend replica for each test, meaning all replicas write to the same temp directory hierarchy. Under extreme counts, this could exhaust temp directory space.