---
{
  "title": "Quick Integration Validation Test",
  "summary": "A 30-minute integration test that provides a fast end-to-end validation of Litestream's replication and restore pipeline. Designed as a pre-commit or pre-release gate that is slower than unit tests but much faster than overnight soak tests.",
  "concepts": [
    "quick validation",
    "30-minute test",
    "pre-release gate",
    "end-to-end",
    "replication",
    "restore",
    "GetTestDuration",
    "GenerateLoad",
    "PrintTestSummary",
    "integration build tag"
  ],
  "categories": [
    "testing",
    "integration",
    "litestream",
    "test"
  ],
  "source_docs": [
    "e31bfb006fa2ca49"
  ],
  "backlinks": null,
  "word_count": 213,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestQuickValidation` runs a condensed version of the overnight file replication test: 10 MB initial population, 30 minutes of load, restore, and validation. It fills the gap between unit tests (milliseconds) and overnight tests (8 hours), giving developers a 30-minute signal on whether the full pipeline is working before committing or deploying.

## Test Flow

1. Creates a database and populates it with 10 MB of data.
2. Starts Litestream replication to a temp directory.
3. Generates load for the configured duration using `db.GenerateLoad`.
4. Stops Litestream, runs `litestream restore`, and validates row counts.
5. Defers `PrintTestSummary` for a run report in the test log.

## Duration Control

`GetTestDuration` defaults to 30 minutes but respects an environment variable override. The test skips in `-short` mode. This makes it practical to run as part of a pre-release checklist on a CI machine with a 1-hour job timeout.

## Scope

Unlike the comprehensive overnight test, `TestQuickValidation` does not enable multi-level compaction or explicit snapshot generation. It tests only the primary replication+restore path, keeping runtime predictable and failure diagnosis simple.

## Known Gaps

- Does not test compaction, snapshots, or retention — those require the overnight or soak tests.
- 30 minutes is too short to observe slow memory leaks or gradual file handle accumulation.