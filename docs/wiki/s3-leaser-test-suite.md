---
{
  "title": "S3 Leaser Test Suite",
  "summary": "Tests the S3 distributed lease implementation using an in-process HTTP test server that simulates S3 conditional write semantics. Covers acquire, renew, release, race conditions, and error guard cases without requiring a real S3 bucket.",
  "concepts": [
    "httptest.Server",
    "S3 conditional write",
    "If-None-Match",
    "ETag",
    "412 Precondition Failed",
    "lease acquire",
    "lease renew",
    "lease release",
    "concurrent acquisition",
    "race condition",
    "atomic",
    "mock S3",
    "ErrLeaseAlreadyReleased"
  ],
  "categories": [
    "testing",
    "storage backends",
    "coordination",
    "test"
  ],
  "source_docs": [
    "a4df11c51efb5beb"
  ],
  "backlinks": null,
  "word_count": 410,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

The test suite for `s3.Leaser` uses `net/http/httptest.NewServer` to simulate S3 without a live bucket. The test server implements `GET`, `PUT`, and `DELETE` handlers with ETag tracking and conditional header evaluation, making the tests deterministic and fast while exercising the same protocol paths the real S3 service would use.

## Acquire Tests

`TestLeaser_AcquireLease_NewLease` verifies that acquiring on an empty bucket sends a PUT with `If-None-Match: *`. The test asserts both the HTTP header and the unmarshalled lease payload (generation field).

`TestLeaser_AcquireLease_ExpiredLease` simulates a lock file whose timestamp is in the past. The test verifies that the leaser overwrites it rather than reporting a conflict.

`TestLeaser_AcquireLease_ActiveLease` returns a lease with a future expiry owned by a different process. The test asserts that `AcquireLease` returns an error, not a successful lease object.

`TestLeaser_AcquireLease_RaceCondition412` simulates the scenario where two leasers simultaneously see an empty bucket. The test server accepts the first PUT and returns 412 to the second, verifying that the losing caller receives an error rather than falsely believing it holds the lease. This test uses `sync/atomic` to control which request wins.

## Renew Tests

`TestLeaser_RenewLease` verifies a successful renewal updates the expiry. `TestLeaser_RenewLease_LostLease` makes the server return 412 on the renewal PUT, simulating a lease stolen by another holder. The test asserts an error is returned so the caller knows to stop treating itself as the leader.

Guard tests (`TestLeaser_RenewLease_NilLease`, `TestLeaser_RenewLease_EmptyETag`) verify that missing ETag or nil lease pointer returns the appropriate sentinel error before any HTTP call is made.

## Release Tests

`TestLeaser_ReleaseLease` verifies the DELETE is issued with the correct ETag. `TestLeaser_ReleaseLease_StaleETag` returns 412 on delete, confirming the leaser surfaces this as an error. `TestLeaser_ReleaseLease_AlreadyDeleted` returns 404, verifying `ErrLeaseAlreadyReleased` is returned. Guard tests again check nil-lease and empty-ETag pre-conditions.

## Concurrency Test

`TestLeaser_ConcurrentAcquisition` spawns multiple goroutines all attempting to acquire simultaneously. The test server enforces real mutual exclusion using a mutex, and the test asserts that exactly one goroutine succeeds. This is the highest-value test in the suite because it catches off-by-one errors in the 412 handling path that single-threaded tests miss.

## Helper

`newTestLeaser` wires a `Leaser` to the test server URL, using a short TTL (1 second) to make expiry tests fast without sleeps.

## Known Gaps

The test server does not simulate S3 eventual consistency delays; all reads see writes immediately. The concurrent test's server-side mutex also serializes more than real S3 would, so transient 503/429 responses from S3 under high contention are not exercised.