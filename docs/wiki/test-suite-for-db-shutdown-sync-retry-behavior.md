---
{
  "title": "Test Suite for DB Shutdown Sync Retry Behavior",
  "summary": "Tests that `DB.Close()` retries the final replica sync on transient failures and gives up after the configured timeout, using a mock replica client to inject controlled failures. Validates the core data-safety guarantee that litestream provides: no committed transactions are lost during normal shutdown.",
  "concepts": [
    "DB.Close",
    "shutdown sync retry",
    "ShutdownSyncTimeout",
    "ShutdownSyncInterval",
    "mock.ReplicaClient",
    "transient failure",
    "rate limiting",
    "sync/atomic",
    "data safety guarantee",
    "WriteLTXFile",
    "LTXFilesFunc",
    "retry loop"
  ],
  "categories": [
    "testing",
    "litestream",
    "replication",
    "test"
  ],
  "source_docs": [
    "551806a5a5db6194"
  ],
  "backlinks": null,
  "word_count": 394,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

`TestDB_Close_SyncRetry` verifies the shutdown sync retry loop in `DB.Close()`. This loop is litestream's primary mechanism for ensuring that all committed transactions reach the remote replica before the daemon exits. Without it, a transient storage failure (rate limiting, network blip) during the final sync would cause data loss.

## SucceedsAfterTransientFailure

This subtest simulates a common real-world scenario: the storage backend is temporarily rate-limiting writes (HTTP 429). The test:

1. Creates a database, writes a table, and syncs to produce an LTX file
2. Installs a mock `ReplicaClient` whose `WriteLTXFile` fails with `"rate limited (429)"` for the first two calls and succeeds on the third
3. Configures `ShutdownSyncTimeout = 5s` and `ShutdownSyncInterval = 50ms`
4. Calls `db.Close()` and asserts that it returns nil (success)
5. Confirms at least 3 write attempts were made

The `sync/atomic` counter is used for thread-safe attempt counting because the retry loop runs in a different goroutine from the test assertion. Using a non-atomic counter would be a data race.

The 50ms interval and 5-second timeout give the loop 100 potential retry attempts, far more than the 3 needed. This prevents the test from being flaky due to timing.

## FailsAfterTimeout

This subtest confirms that the retry loop does give up. A mock client that always fails is installed, with a very short `ShutdownSyncTimeout` (tens of milliseconds). `Close()` should return a non-nil error after exhausting the timeout, confirming that litestream does not hang forever if the replica is permanently unavailable during shutdown.

## Why ShutdownSyncTimeout and ShutdownSyncInterval Exist

These two fields provide operator control over the shutdown behavior trade-off:

- A longer timeout provides more time for transient failures to resolve, reducing data loss risk
- A shorter timeout makes the daemon shut down faster, at the cost of potentially not flushing the final sync
- The interval controls how quickly retries happen, balancing retry frequency against wasted API calls

## Mock Infrastructure

The test uses `mock.ReplicaClient`, a generated mock struct with function-field callbacks for each interface method. `LTXFilesFunc` returns an empty iterator (no remote files) so the sync always has LTX data to upload. `WriteLTXFileFunc` contains the failure injection logic.

## Known Gaps

No test for the case where `Done` channel is closed (Ctrl+C) during the shutdown sync loop. The expected behavior is that the loop stops immediately rather than waiting for the timeout, but this is not verified.