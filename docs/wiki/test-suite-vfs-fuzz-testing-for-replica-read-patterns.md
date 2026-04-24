---
{
  "title": "Test Suite: VFS Fuzz Testing for Replica Read Patterns",
  "summary": "fuzz_test.go uses Go's built-in fuzzing engine to generate random read workloads against the Litestream VFS replica, exercising aggregate queries, ordering, and mixed access patterns. A deterministic seed corpus ensures the fuzz logic also runs under standard `go test` without `-fuzz`.",
  "concepts": [
    "fuzz testing",
    "VFS replica",
    "seed corpus",
    "Go fuzzing",
    "read patterns",
    "MVCC",
    "SQLite VFS",
    "pseudo-random workload",
    "build tags",
    "regression testing"
  ],
  "categories": [
    "testing",
    "litestream",
    "fuzzing",
    "VFS",
    "test"
  ],
  "source_docs": [
    "37b982df449ffbcd"
  ],
  "backlinks": null,
  "word_count": 312,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

The VFS layer interprets page requests from SQLite and assembles them from LTX file streams. There are many ways this assembly can go wrong: out-of-order page delivery, stale cache entries, incorrect MVCC snapshot handling. Fuzz testing drives the system with unexpected input combinations that unit tests would not generate.

## Dual-Mode Design

`TestVFS_FuzzSeedCorpus` and `FuzzVFSReplicaReadPatterns` share the `runVFSFuzzWorkload` helper. This dual-mode approach is a Go best practice:

- `TestVFS_FuzzSeedCorpus` runs a handful of fixed byte sequences under standard `go test`, so the fuzz logic is always exercised in CI without requiring `-fuzz`.
- `FuzzVFSReplicaReadPatterns` extends the same logic to the full fuzzing engine when invoked with `go test -fuzz=FuzzVFSReplicaReadPatterns -tags vfs`.

This prevents the common problem where fuzz tests are written but never run because they require a special invocation.

## runVFSFuzzWorkload

The corpus bytes are used as a seed for pseudo-random decisions: which query to run (SELECT with aggregate, ORDER BY, GROUP BY), how many rows to fetch, and which column to filter on. The query mix is derived deterministically from the corpus bytes, so a failing corpus can be replayed exactly by re-running with the same bytes.

The workload uses a real file-backend replica and a real SQLite VFS connection, so failures indicate genuine bugs rather than test infrastructure issues.

## Build Tag

```go
//go:build vfs
```

The `vfs` tag gates both the seed corpus test and the fuzz harness. This is important because the VFS integration requires CGO (via `sqlite3vfs`) and is not available on all platforms or in all build configurations.

## Known Gaps

- The corpus bytes currently drive only read patterns. Write patterns (INSERT, UPDATE during read) are not fuzz-tested, leaving concurrent write races outside the fuzzer's reach.
- The fuzz harness does not persist crash-reproducing corpora to the `testdata/fuzz/` directory automatically when run in CI, so regressions would need to be reproduced manually.