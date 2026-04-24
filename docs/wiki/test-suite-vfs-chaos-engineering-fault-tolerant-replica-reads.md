---
{
  "title": "Test Suite: VFS Chaos Engineering — Fault-Tolerant Replica Reads",
  "summary": "chaos_test.go injects random read failures into the Litestream VFS layer and verifies that the replica database remains queryable despite transient storage errors. It uses a wrapping `chaosReplicaClient` that probabilistically returns errors on `LTXFiles` and `OpenLTXFile` calls.",
  "concepts": [
    "chaos engineering",
    "fault injection",
    "VFS replica",
    "transient errors",
    "chaosReplicaClient",
    "Litestream VFS",
    "build tags",
    "atomic counters",
    "page integrity",
    "retry logic",
    "fault tolerance"
  ],
  "categories": [
    "testing",
    "litestream",
    "chaos",
    "VFS",
    "test"
  ],
  "source_docs": [
    "a7e39dd3298bd723"
  ],
  "backlinks": null,
  "word_count": 432,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The VFS (Virtual File System) layer lets SQLite queries run directly against a Litestream replica without a full restore. In production, the underlying storage (S3, Azure Blob, GCS) experiences transient errors: throttling, network blips, and partial reads. The chaos test verifies that the VFS handles these faults gracefully — retrying internally, returning consistent data, and never exposing SQLite to a corrupt page.

## Build Tags

```go
//go:build vfs && chaos
```

The test requires both the `vfs` and `chaos` build tags, meaning it only runs when explicitly requested (`go test -tags "vfs chaos"`). This is deliberate: chaos tests are non-deterministic by nature and would be noise in standard CI. They are run periodically in dedicated fault-tolerance pipelines.

## chaosReplicaClient

`chaosReplicaClient` wraps a real `file.ReplicaClient` and intercepts `LTXFiles` and `OpenLTXFile` calls. On each call, it generates a random number; if it falls below the configured failure probability, it returns an error instead of delegating to the real client.

```go
type chaosReplicaClient struct {
    rnd      *rand.Rand
    failures atomic.Int64
    active   bool  // toggles chaos on/off
}
```

The `active` toggle allows the test to phase chaos on after the replica has had a chance to hydrate its initial snapshot, avoiding spurious failures during the setup phase.

## Test Flow

1. A primary database is opened with a file replica and seeded with 64 rows across 8 groups.
2. A `chaosReplicaClient` is created wrapping the file replica.
3. A VFS is opened against the chaos client with a 15ms poll interval.
4. Read queries run against the VFS replica concurrently with writes to the primary.
5. The test asserts that queries succeed despite the chaos client returning errors on some calls.
6. At the end, `chaosClient.failures` is checked to confirm that at least some errors were actually injected — otherwise the test would pass vacuously if the chaos never fired.

## What Failure Tolerance Looks Like

The VFS must retry failed LTX reads internally, fall back to cached pages where possible, and never return a partial or corrupt page to SQLite. A page tear — where half a page is from one transaction and half from another — would cause SQLite to crash with a checksum error. The chaos test verifies this does not happen.

## Known Gaps

- The chaos client injects failures uniformly at random. It does not simulate correlated failures (e.g., a whole page of consecutive reads failing) which is the more dangerous failure mode in practice.
- The failure probability is hardcoded in the test rather than being configurable, limiting the ability to tune the aggression level without code changes.