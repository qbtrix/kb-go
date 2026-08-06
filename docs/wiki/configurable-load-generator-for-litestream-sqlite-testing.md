---
{
  "title": "Configurable Load Generator for Litestream SQLite Testing",
  "summary": "LoadCommand drives concurrent read/write load against a SQLite database to stress-test Litestream replication under realistic workloads. It supports configurable write rates, mixed read/write ratios, multiple worker goroutines, and wave-function traffic shaping to simulate bursty production patterns.",
  "concepts": [
    "load testing",
    "SQLite stress test",
    "concurrent workers",
    "atomic counters",
    "wave function",
    "traffic shaping",
    "write rate",
    "read/write ratio",
    "payload generation",
    "Litestream testing",
    "WAL pressure"
  ],
  "categories": [
    "litestream",
    "testing",
    "load generation",
    "SQLite"
  ],
  "source_docs": [
    "150e35181f214cdf"
  ],
  "backlinks": null,
  "word_count": 463,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

A replication system that works under constant low load may fail under bursty production traffic — lock contention, WAL growth, compaction lag, and replica divergence all surface under stress. `LoadCommand` exists to reproduce those conditions reliably so regressions can be caught before they reach production.

## Worker Pool Architecture

`generateLoad` spawns `Workers` goroutines, each running an independent `worker` loop. Workers share a single `*sql.DB` connection pool (SQLite's default pool size) rather than each holding their own connection. This is intentional: it matches how real applications behave and surfaces contention patterns that per-connection designs would hide.

Each worker decides whether to read or write based on `ReadRatio` (a float between 0 and 1). A random draw per operation means the actual read/write mix follows a binomial distribution around the target ratio.

## Traffic Shaping

`waveFunction` computes a multiplier in [0.5, 1.5] using a sine approximation:

```
multiplier = 1.0 + 0.5 * sin(2π * t / period)
```

The `sinApprox` function uses a polynomial approximation rather than `math.Sin` to avoid importing the math package. Applied to the target `WriteRate`, this creates a smooth oscillation between 50% and 150% of the configured rate, which stresses the system's ability to handle load spikes without requiring external traffic generation tools.

## Atomic Statistics

`LoadStats` uses `atomic.Int64` counters for `writes`, `reads`, and `errors`. This avoids mutex overhead in the hot path where hundreds of operations per second are being counted. The `lastReport` timestamp uses a mutex-protected field because it is read and written by the reporter goroutine and needs exact consistency for rate calculations.

`calculateRate` computes ops/second since the last report by taking the atomic snapshot, computing the delta, and dividing by the elapsed duration. This gives per-interval throughput rather than cumulative average, which is more useful for spotting degradation over time.

## Payload Generation

`performWrite` generates a random byte payload using `crypto/rand` (cryptographically secure randomness) rather than `math/rand`. This prevents SQLite from exploiting any regularity in the data (e.g., page-level compression that would make the database size unrepresentative). The payload size matches `PayloadSize`, defaulting to a configurable number of bytes per row.

## Shutdown

The command installs a `SIGINT`/`SIGTERM` handler and cancels the context when a signal arrives. Workers check `ctx.Done()` at the top of each iteration and exit cleanly. `finalReport` prints cumulative statistics after all workers finish, giving the operator a summary of total throughput and error rate for the run.

## Known Gaps

- `waveFunction` uses a Taylor-series sine approximation that loses accuracy for large `t` values. For very long test runs (hours), the wave shape may drift from a perfect sine.
- There is no backpressure mechanism: if the database cannot keep up with the configured `WriteRate`, errors accumulate rather than the rate being throttled. This can mask the true throughput ceiling.