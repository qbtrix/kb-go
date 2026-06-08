---
{
  "title": "Test Suite for Heartbeat Client",
  "summary": "Tests the HeartbeatClient's HTTP ping behavior, interval scheduling logic, minimum interval clamping, and integration with the Store's health monitoring loop.",
  "concepts": [
    "HeartbeatClient",
    "httptest",
    "ShouldPing",
    "RecordPing",
    "MinHeartbeatInterval",
    "context cancellation",
    "Store heartbeat",
    "integration test",
    "atomic counter",
    "HTTP method verification"
  ],
  "categories": [
    "testing",
    "monitoring",
    "litestream",
    "test"
  ],
  "source_docs": [
    "e46edbba86a10dc9"
  ],
  "backlinks": null,
  "word_count": 241,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This test file covers the `HeartbeatClient` across all of its behavioral contracts: correct HTTP method, error propagation, interval clamping, scheduling state, and integration with the broader Store healthcheck loop.

## Ping Tests

`TestHeartbeatClient_Ping` covers five sub-cases using `httptest.NewServer` to spin up in-process HTTP servers:

- **Success**: Verifies a GET is issued (not POST/PUT) and the server receives exactly one request.
- **EmptyURL**: Confirms that an empty URL returns nil immediately without making any HTTP call—this is the disabled/no-op path.
- **NonSuccessStatusCode**: An HTTP 500 response must produce an error so missed pings are detected.
- **NetworkError**: Dialing an unreachable port (`localhost:1`) must return an error.
- **ContextCanceled**: A pre-cancelled context must fail immediately, even if the server is slow. This prevents heartbeat goroutines from blocking process shutdown.

## Scheduling Tests

`TestHeartbeatClient_ShouldPing` verifies two invariants:
- A freshly created client with zero `lastPingAt` always reports `ShouldPing() = true`, enabling the first ping to fire immediately on startup.
- After `RecordPing()`, `ShouldPing()` returns false (the interval has not elapsed yet).

`TestHeartbeatClient_MinInterval` directly asserts that passing `30*time.Second` (below the 1-minute floor) results in the client's `Interval` field being set to `MinHeartbeatInterval`. This catches regressions in the constructor's clamping logic.

## Store Integration Test

`TestStore_Heartbeat_AllDatabasesHealthy` uses `testingutil` to open real SQLite databases and run litestream's Store heartbeat path end-to-end, verifying that pings are dispatched when all replicas are healthy. This test requires the `testingutil` integration flag to be set for backends requiring credentials.