---
{
  "title": "Litestream Shutdown Sync Retry Test Under 429 Rate Limiting",
  "summary": "Tests that Litestream retries flushing pending LTX files to object storage when the storage endpoint returns HTTP 429 (Too Many Requests) during graceful shutdown. An in-process reverse proxy injects the rate-limit responses so the test runs without modifying the storage layer.",
  "concepts": [
    "graceful shutdown",
    "429 rate limiting",
    "shutdown sync retry",
    "reverse proxy",
    "SIGTERM",
    "LTX files",
    "object storage",
    "atomic counters",
    "in-process proxy",
    "litestream",
    "SQLite WAL"
  ],
  "categories": [
    "integration-testing",
    "resilience",
    "litestream",
    "storage",
    "test"
  ],
  "source_docs": [
    "974383e7ba2ecdea"
  ],
  "backlinks": null,
  "word_count": 471,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestShutdownSyncRetry_429Errors` exists because graceful shutdown is a correctness boundary: if Litestream exits without persisting buffered WAL data, that data is permanently lost. Cloud object stores enforce request quotas, so a brief burst of uploads during shutdown could trigger 429 responses — exactly the moment when retrying is most critical and hardest to test.

## In-Process Rate-Limiting Proxy

Rather than relying on a separate proxy container, the test runs a `rateLimitingProxy` entirely within the test process. The struct wraps `httputil.ReverseProxy` and intercepts PUT requests:

```go
type rateLimitingProxy struct {
    target      *url.URL
    proxy       *httputil.ReverseProxy
    putCount    int32
    limit       int32
    totalReqs   int64
    rateLimited int64
    forwarded   int64
    ...
}
```

`ServeHTTP` uses atomic counters to return 429 for the first `limit` PUT requests and then forward subsequent requests normally. The `Reset()` method zeroes the counter so the proxy can be reused: the test resets it just before sending SIGTERM so that the 429s fire specifically during the shutdown sync, not during initial replication.

## Configuration: Shutdown Sync Timeout

The Litestream config written for this test includes two fields that control shutdown retry behavior:

```yaml
shutdown-sync-timeout: 10s
shutdown-sync-interval: 500ms
```

These settings tell Litestream to keep retrying failed uploads for up to 10 seconds at 500 ms intervals. Without them, Litestream would attempt a single sync and exit on failure. The test validates that these config knobs actually wire through to the retry loop.

## Shutdown Sequence

1. Write initial data, start Litestream, wait for initial sync.
2. Write 5 more rows so there are pending LTX files to flush.
3. Call `proxy.Reset()` to arm the 429 counter.
4. Send `syscall.SIGTERM` to the Litestream process.
5. Wait up to 30 seconds for the process to exit.

The test then inspects combined stdout/stderr for the string `"shutdown sync failed, retrying"` as evidence that the retry loop ran, and `"shutdown sync succeeded after retry"` as evidence it succeeded.

## Observability

`proxy.Stats()` returns a snapshot of all three counters: total requests, rate-limited responses, and forwarded requests. These are logged regardless of pass/fail, which makes the test output useful as a diagnostic when the retry mechanism is not triggered (e.g., because no LTX files were pending at shutdown time).

## Known Gaps

- The test accepts a warning path: if no retry messages appear in the output, it logs a warning but does not call `t.Fatal`. This means the test passes even when the retry logic is never exercised, which could mask a regression where retry is silently disabled.
- The 429 limit is hardcoded to 3 in the test body. If upstream changes reduce the number of PUT requests needed to flush a small database, the proxy may never return a 429.
- The test uses `time.Sleep` for synchronization and a fixed 30-second timeout. On slow CI hosts, 30 seconds may be insufficient if MinIO startup is delayed.