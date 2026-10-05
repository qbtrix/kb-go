---
{
  "title": "Test Suite: VFS Long-Running Soak Test Under Sustained Load",
  "summary": "vfs_soak_test.go exercises the Litestream VFS under sustained concurrent read/write load for a configurable duration (default 5 minutes), looking for goroutine leaks, memory growth, and consistency errors that only manifest over time.",
  "concepts": [
    "soak test",
    "sustained load",
    "goroutine leak",
    "memory growth",
    "atomic counters",
    "row count monotonicity",
    "VFS replica",
    "duration configuration",
    "build tags",
    "long-running test"
  ],
  "categories": [
    "testing",
    "litestream",
    "VFS",
    "soak",
    "test"
  ],
  "source_docs": [
    "7afe95328b188f15"
  ],
  "backlinks": null,
  "word_count": 392,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Short tests exercise code paths under ideal conditions. The soak test exists because certain failure modes — goroutine leaks from abandoned poll loops, page cache memory growth from unreleased LTX handles, and timestamp clock drift — only appear after minutes or hours of continuous operation. The soak test is the primary signal for these slow-burn regressions.

## Build Tag

```go
//go:build vfs && soak
```

The `soak` tag prevents this test from running in standard CI where its duration (5+ minutes) would be unacceptable. It is intended for nightly or pre-release pipelines.

## Duration Configuration

```go
duration := 5 * time.Minute
if v := os.Getenv("LITESTREAM_VFS_SOAK_DURATION"); v != ""{
    if parsed, err := time.ParseDuration(v); err == nil {
        duration = parsed
    }
}
if testing.Short() && duration > time.Minute {
    duration = time.Minute
}
```

Three tiers of control: the environment variable for pipeline customization, the `-short` flag for faster local verification, and the hardcoded 5-minute default for normal soak runs. The silent fallback when `ParseDuration` fails is a gap — a malformed `LITESTREAM_VFS_SOAK_DURATION` would silently use the default rather than failing fast.

## Workload

1. A primary database is seeded with 1000 rows.
2. A VFS replica opens with a 100ms poll interval (slower than the main test to reduce CPU pressure over long runs).
3. Two goroutines run for the full duration:
   - **Writer**: inserts a row with a random TEXT value every iteration, tracking write count with `atomic.Int64`.
   - **Reader**: runs SELECT queries against the VFS replica and validates that the row count is non-decreasing.
4. After the duration, the test checks that `replica_rows >= initial_rows`, confirming the VFS tracked all committed transactions.

## Row Count Monotonicity

The reader verifies that the row count seen through the VFS never decreases. This would indicate a VFS snapshot regression — the replica rolling back to an earlier state rather than advancing. A decrease would mean the VFS is serving stale data from a cached page that was invalidated but not evicted.

## Known Gaps

- The test does not check for goroutine count growth (`runtime.NumGoroutine`), so goroutine leaks would not be caught unless they cause an OOM.
- Write errors from the primary database are logged but do not fail the test, so a primary that stops writing due to a disk error would be treated as a normal run.