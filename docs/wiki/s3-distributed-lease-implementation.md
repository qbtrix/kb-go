---
{
  "title": "S3 Distributed Lease Implementation",
  "summary": "Implements a distributed advisory lock over S3 using a JSON lease file and S3 conditional writes (If-None-Match / ETag) to provide mutual exclusion. The Leaser allows only one holder at a time to own a generation slot, preventing split-brain replication across multiple Litestream instances.",
  "concepts": [
    "distributed lock",
    "S3 conditional write",
    "If-None-Match",
    "ETag",
    "lease",
    "TTL",
    "mutual exclusion",
    "412 Precondition Failed",
    "Leaser",
    "split-brain prevention",
    "optimistic concurrency",
    "generation"
  ],
  "categories": [
    "replication",
    "storage backends",
    "coordination"
  ],
  "source_docs": [
    "3afed52bb5e6a612"
  ],
  "backlinks": null,
  "word_count": 428,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

The `Leaser` implements the `litestream.Leaser` interface using a single JSON file stored at a configurable path within an S3 bucket. Mutual exclusion is achieved through S3's conditional PUT (`If-None-Match: *`) and conditional DELETE semantics, which S3 evaluates atomically server-side. This avoids the need for a separate coordination service like etcd or ZooKeeper.

## Lease Lifecycle

### Acquire

`AcquireLease` first calls `readLease` to fetch the current lock file and its ETag. Three outcomes are possible:

1. **File not found** — no holder; write a new lease with `If-None-Match: *`. If another writer races and creates the file first, S3 returns 412 Precondition Failed and the acquire fails cleanly.
2. **Expired lease** — the TTL has elapsed; overwrite using the ETag of the stale lease as the `If-Match` precondition, preventing a third party from acquiring between the read and write.
3. **Active lease held by another owner** — return an error immediately; the caller is responsible for retrying after the TTL.

The 412 Precondition Failed response is the key atomicity primitive. `isPreconditionFailed` detects this error from the AWS SDK's error type hierarchy so the caller can distinguish a racing writer from a genuine S3 error.

### Renew

`RenewLease` extends the lease's expiry by writing a new TTL using the current ETag as a precondition. If another process has acquired the lease (different ETag), the write returns 412 and the renew fails, signaling to the caller that it has lost the lease — typically triggering a shutdown or re-election.

### Release

`ReleaseLease` deletes the lock file using the ETag as a precondition. If the ETag no longer matches, the lease has already been stolen and the release is a no-op. `ErrLeaseAlreadyReleased` is returned when the file is gone (404), allowing the caller to detect whether the release happened cleanly.

## ETag as Version Token

The ETag returned by S3 on every read or write is stored in the `litestream.Lease` struct and passed back on every mutating call. This is functionally equivalent to an optimistic concurrency version counter. Without it, a slow GC pause could cause a process to overwrite a lease that a new holder has already acquired.

## Configuration

- `TTL` defaults to `DefaultLeaseTTL` (30 seconds). The holder must renew before expiry.
- `Path` defaults to `lock.json` at the replica root.
- `Owner` identifies the lease holder in the JSON payload for debugging.

## Known Gaps

- No retry logic inside `AcquireLease` — the caller must implement its own back-off loop.
- `DefaultLeaseTTL` is fixed at 30 seconds; there is no dynamic adjustment based on observed renewal latency.