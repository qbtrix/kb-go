---
{
  "title": "Litestream Soak Test Helper Library",
  "summary": "A shared helper library used by long-running soak tests that exercise Litestream under sustained load. It handles MinIO container lifecycle, AWS credential probing, dynamic config generation, progress monitoring, signal handling for interactive runs, and post-run analysis of compaction and replication metrics.",
  "concepts": [
    "soak test",
    "MinIO",
    "Docker lifecycle",
    "AWS credentials",
    "compaction analysis",
    "signal handling",
    "progress monitoring",
    "error categorization",
    "LTX files",
    "litestream",
    "long-running tests",
    "config generation"
  ],
  "categories": [
    "integration-testing",
    "litestream",
    "storage",
    "observability",
    "test"
  ],
  "source_docs": [
    "e9973385c8324ac3"
  ],
  "backlinks": null,
  "word_count": 534,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

Soak tests differ from unit or integration tests: they run for minutes to hours to expose memory leaks, drift, and compaction imbalances that only appear under sustained pressure. `soak_helpers.go` (build tag `integration && soak`) provides the shared infrastructure that all soak scenarios rely on.

## MinIO Lifecycle Management

`StartMinIOContainer` and `StopMinIOContainer` wrap Docker commands to spin up a local S3-compatible store. The container is given a timestamped name to prevent collisions between concurrent test runs. `StopMinIOContainer` also removes the Docker volume to avoid disk exhaustion across many test runs.

`waitForMinIOBucket` polls until the bucket is accessible or a timeout is reached — this addresses the window between container start and MinIO's internal initialization.

## AWS Credential Probing

`CheckAWSCredentials` reads standard AWS environment variables and returns the target bucket and region. It calls `t.Skip` rather than `t.Fatal` when credentials are absent, so soak tests that require real S3 degrade gracefully in CI environments without cloud access. `TestS3Connectivity` then performs a lightweight test write-and-delete to confirm reachability before a multi-hour run begins.

## Dynamic Config Generation

`CreateSoakConfig` writes a Litestream YAML config file that accounts for two modes:

- **Short mode** — smaller retention windows and higher sync frequency for quick smoke tests.
- **Full soak mode** — production-like retention and compaction intervals.

The config is written to a temp path derived from the database path, ensuring each test run gets an isolated config without file-naming conflicts.

## Signal Handling

`setupSignalHandler` installs a `SIGINT`/`SIGTERM` handler that calls `performGracefulShutdown` before exit. This is important for interactive soak runs where the operator presses Ctrl-C: without the handler, the MinIO container and Docker network would be left running. `promptYesNo` detects whether stdin is a terminal (`isInteractive`) and uses environment variables (`SOAK_AUTO_PURGE`) as override flags so automated pipelines can skip interactive prompts.

## Progress and Error Monitoring

`MonitorSoakTest` runs a goroutine that calls a user-supplied metrics function every 60 seconds. `printProgress` renders a text progress bar with elapsed time, row counts, and an error summary.

`getErrorStats` categorizes Litestream log lines into:

- **Critical errors** — replication gaps, checkpoint failures, corruption indicators.
- **Benign errors** — transient network hiccups or lock contention that self-resolve.

`shouldAbortTest` returns true when critical error counts exceed a threshold or no files have been created after a warm-up window, enabling automated early exit without operator intervention.

## Post-Run Analysis

`AnalyzeSoakTest` parses the Litestream log file to extract:

- Compactions per level (L0-L3).
- Snapshot and checkpoint counts.
- Transaction ID range (`MinTxID`, `MaxTxID`).
- Final database size.

`PrintSoakTestAnalysis` formats these into a plain-English summary that is easy to read in CI logs. This analysis is the primary artifact that tells whether a soak run indicates a regression.

## Known Gaps

- `parseLog` is a line-by-line string scanner, not a structured log parser. It will break silently if Litestream changes its log message format.
- `CountMinIOObjects` and `GetS3StorageSize` shell out to `docker exec` and `aws s3 ls`; they do not work if Litestream is configured to use a non-Docker MinIO or a non-standard AWS CLI path.
- The interactive prompt machinery (`promptYesNo`) has a default-yes/default-no split that is easy to confuse; the naming convention (`promptYesNoDefaultNo` vs `promptYesNoDefaultYes`) helps but no compile-time check enforces correct usage.