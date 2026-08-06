---
{
  "title": "Idle CPU Profiling Test",
  "summary": "A manual profiling harness that starts N idle Litestream databases with file replicas and exposes pprof endpoints for interactive CPU and heap profiling. Used to measure per-database overhead when running many Litestream instances on a single machine.",
  "concepts": [
    "pprof",
    "CPU profiling",
    "idle overhead",
    "PROFILE_DB_COUNT",
    "modernc sqlite",
    "build tag profile",
    "goroutine dump",
    "heap profile",
    "signal handling",
    "multitenant",
    "interactive profiling"
  ],
  "categories": [
    "testing",
    "profiling",
    "litestream",
    "test"
  ],
  "source_docs": [
    "b22cfde42d52881f"
  ],
  "backlinks": null,
  "word_count": 311,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestIdleCPUProfile` is not an automated correctness test — it is an interactive profiling session gated by the `profile` build tag. It starts a configurable number of databases with no write load and exposes `pprof` HTTP endpoints so a developer can capture profiles while the system is in a known idle state.

## Purpose

When deploying Litestream to replicate hundreds of SQLite databases on a single host (e.g., in a multitenant SaaS), per-database CPU overhead compounds. This test provides a repeatable environment for measuring that overhead and identifying hot loops in the monitoring, sync, or compaction goroutines during idle periods — which are otherwise masked by write traffic in load tests.

## Configuration

The number of databases is controlled by the `PROFILE_DB_COUNT` environment variable, defaulting to 100. Each database gets a dedicated file-based replica in its own temp directory.

## pprof Endpoints

The test imports `net/http/pprof` as a side effect, which registers profiling endpoints on the default `http.ServeMux`. The server listens on a configurable port (default `:6060`), exposing:
- `GET /debug/pprof/profile` — 30-second CPU profile.
- `GET /debug/pprof/heap` — heap snapshot.
- `GET /debug/pprof/goroutine` — goroutine dump.

## Signal Handling

The test blocks on `os.Signal` (SIGINT/SIGTERM) rather than a timer so the developer can capture profiles interactively and then terminate. This is the correct approach for a profiling harness — a timer would end the session before the developer finishes analysis.

## SQLite Driver

The test imports `modernc.org/sqlite` (a pure-Go SQLite driver) rather than `mattn/go-sqlite3` (cgo-based). The pure-Go driver avoids cgo function call overhead appearing in CPU profiles, which would obscure Litestream's own overhead.

## Known Gaps

- The test does not measure per-database memory overhead, only CPU. Heap profiles are available via pprof but not automatically captured and compared.
- No baseline measurement from a previous build is captured for regression comparison — the developer must do this manually.