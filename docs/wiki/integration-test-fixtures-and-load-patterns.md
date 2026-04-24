---
{
  "title": "Integration Test Fixtures and Load Patterns",
  "summary": "Defines reusable data fixtures, load pattern configurations, and test scenario templates for integration tests. Provides configurable write patterns (constant, burst, random, wave) and database population utilities that create realistic schemas and data volumes.",
  "concepts": [
    "LoadPattern",
    "LoadConfig",
    "PopulateConfig",
    "test fixtures",
    "data generation",
    "write patterns",
    "burst",
    "sinusoidal",
    "schema",
    "TestScenario",
    "WAL",
    "checkpoint",
    "PayloadSize",
    "Workers"
  ],
  "categories": [
    "testing",
    "integration",
    "infrastructure",
    "test"
  ],
  "source_docs": [
    "7da0358c1d989798"
  ],
  "backlinks": null,
  "word_count": 370,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

This file provides the data layer for integration tests. Rather than each test hardcoding its own schema and write logic, they use these fixtures to produce consistent, realistic workloads.

## Load Patterns

`LoadPattern` is a string constant type with four values:

- `LoadPatternConstant` — steady write rate, simulating a predictable application.
- `LoadPatternBurst` — alternating high and zero write rates, simulating a batched ETL job.
- `LoadPatternRandom` — random inter-write delays, simulating a bursty web application.
- `LoadPatternWave` — sinusoidal write rate, simulating diurnal traffic patterns.

These patterns exist so soak and endurance tests are not just testing Litestream under ideal conditions but under the varied timing that real applications produce.

## LoadConfig

`LoadConfig` parametrizes `GenerateLoad` on the `TestDB`. Fields include:
- `WriteRate` — target writes per second.
- `Duration` — how long to generate load.
- `Pattern` — which `LoadPattern` to use.
- `PayloadSize` — bytes per row, controlling WAL segment sizes.
- `ReadRatio` — fraction of operations that are reads, exercising read/write contention.
- `Workers` — goroutine count for concurrent write pressure.

`DefaultLoadConfig` provides sensible defaults so most tests can use it without customization.

## PopulateConfig

`PopulateConfig` controls database population for tests that need a pre-existing large database:
- `TargetSize` — target database size in bytes.
- `RowSize` — approximate bytes per row.
- `BatchSize` — rows per transaction, balancing WAL segment size vs. transaction overhead.
- `TableCount` — number of tables to spread data across.
- `IndexRatio` — fraction of tables with secondary indexes.
- `PageSize` — SQLite page size, allowing tests to target specific page-size scenarios.

## Complex Schema

`CreateComplexTestSchema` creates a multi-table schema with foreign key relationships (users, posts, comments, tags, categories). `PopulateComplexTestData` fills it with realistic row counts. This schema exercises joins and foreign key constraint enforcement during integrity checks, which catches replication bugs that only appear with relational data.

## Test Scenarios

`TestScenario` is a struct with `Setup` and `Validate` function fields. `LargeWALScenario` returns a scenario that generates a WAL file exceeding the default checkpoint threshold. `RapidCheckpointsScenario` generates rapid WAL-checkpoint cycles.

## Known Gaps

- `generateRandomContent` uses `crypto/rand` for payload bytes — this is correct for randomness but slower than `math/rand`. For high-rate load generation this may become a CPU bottleneck.