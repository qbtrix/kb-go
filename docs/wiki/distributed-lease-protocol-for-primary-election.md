---
{
  "title": "Distributed Lease Protocol for Primary Election",
  "summary": "Defines the Leaser interface and Lease data structure used by litestream to implement distributed leadership election. Only one replica node at a time holds the lease and is permitted to write LTX files; other nodes wait to acquire the lease if the holder crashes or stops renewing.",
  "concepts": [
    "lease",
    "distributed lock",
    "primary election",
    "Leaser interface",
    "ETag",
    "optimistic concurrency",
    "LeaseExistsError",
    "ErrLeaseNotHeld",
    "generation counter",
    "TTL",
    "failover"
  ],
  "categories": [
    "distributed systems",
    "replication",
    "litestream"
  ],
  "source_docs": [
    "86026b64494febb4"
  ],
  "backlinks": null,
  "word_count": 326,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Overview

In a multi-node litestream deployment, only one node should be actively replicating the SQLite database at any given time. The lease system enforces this by making nodes compete for an exclusive time-bounded token stored in a shared backend (e.g., an S3 object with an ETag-based conditional write, or a Redis key).

## Lease Structure

A `Lease` carries:
- `Generation int64` — monotonically increasing counter. Each time the lease changes hands, the generation increments, allowing downstream consumers to detect leader transitions.
- `ExpiresAt time.Time` — when the lease expires if not renewed. Short TTLs (e.g., 30 seconds) mean failover is quick, but renewals must be frequent.
- `Owner string` — optional human-readable identity of the lease holder (hostname, pod name, etc.).
- `ETag string` — used for optimistic concurrency in backends like S3. The ETag of the object at the time the lease was acquired is stored and presented on renewal to ensure no other node has written in the interim.

`IsExpired()` and `TTL()` are convenience methods for scheduler loops that check freshness before acting.

## Leaser Interface

```go
type Leaser interface {
    Type() string
    AcquireLease(ctx context.Context) (*Lease, error)
    RenewLease(ctx context.Context, lease *Lease) (*Lease, error)
    ReleaseLease(ctx context.Context, lease *Lease) error
}
```

- `AcquireLease` atomically takes the lease if it is unclaimed or expired.
- `RenewLease` extends the expiry of a currently-held lease, using the ETag to detect if another node has stolen it.
- `ReleaseLease` voluntarily surrenders the lease on clean shutdown.

## Error Types

`LeaseExistsError` is returned by `AcquireLease` when another node holds a valid lease. It includes the current holder's identity and expiry time so callers can log meaningful wait messages. `ErrLeaseNotHeld` is returned by `RenewLease` or `ReleaseLease` when the backend confirms the caller no longer owns the lease (e.g., it expired and was taken by another node).

## Known Gaps

No concrete `Leaser` implementation is present in this file—this is the interface-only definition. Implementations (S3-based, file-based, etc.) live in separate packages.