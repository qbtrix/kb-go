---
{
  "title": "Heartbeat Client for Uptime Monitoring",
  "summary": "Provides a lightweight HTTP heartbeat sender that pings an external monitoring URL on a configurable interval to signal that the litestream process is alive. Includes a minimum interval guard to prevent accidental high-frequency pinging.",
  "concepts": [
    "heartbeat",
    "uptime monitoring",
    "HTTP ping",
    "ShouldPing",
    "RecordPing",
    "MinHeartbeatInterval",
    "sync.Mutex",
    "Healthchecks.io",
    "scheduler",
    "context cancellation"
  ],
  "categories": [
    "monitoring",
    "litestream",
    "infrastructure"
  ],
  "source_docs": [
    "ef90f2e79f1a8f89"
  ],
  "backlinks": null,
  "word_count": 333,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

The `HeartbeatClient` sends periodic HTTP GET pings to a configurable URL (e.g., a Healthchecks.io or UptimeRobot endpoint). If litestream crashes or hangs, missed pings cause the monitoring service to fire an alert. The client itself does not run a background goroutine—it exposes `Ping`, `ShouldPing`, and `RecordPing` methods that callers invoke from their own scheduler loops.

## Configuration and Defaults

Three constants govern timing:

- `DefaultHeartbeatInterval = 5 * time.Minute` — how often to ping under normal conditions.
- `DefaultHeartbeatTimeout = 30 * time.Second` — per-request HTTP timeout.
- `MinHeartbeatInterval = 1 * time.Minute` — floor applied in `NewHeartbeatClient` to prevent callers from configuring intervals below one minute.

The minimum interval guard exists because external monitoring services often have rate limits or subscription-tier restrictions, and sub-minute pinging would waste those quotas or get requests blocked.

## Thread-Safe Ping Scheduling

`ShouldPing` and `RecordPing` share a `sync.Mutex` around `lastPingAt`. The decoupled design—check, then do work, then record—means callers control the moment of recording. If the ping HTTP call fails, the caller can choose not to call `RecordPing`, letting `ShouldPing` return true again on the next scheduler tick for an immediate retry. If they do call `RecordPing` on error, they effectively skip until the next interval.

## Ping Mechanics

`Ping` issues a `context`-aware GET request and checks the response status. Empty URL is treated as a no-op (returns nil error), which allows heartbeats to be disabled cleanly by leaving the URL unconfigured without callers needing to nil-check the client. Any non-2xx status code is treated as a failure and returns an error, triggering the monitoring service to see a missed check-in.

## Data Flow

```
Scheduler loop
  → ShouldPing() = true
  → Ping(ctx) — HTTP GET → external service
  → RecordPing() — updates lastPingAt
```

## Known Gaps

No retry logic is built in—a single failed ping is returned as an error to the caller. There is no circuit breaker or exponential back-off for repeated failures. Callers must implement their own retry policy if needed.