---
{
  "title": "Vector Search Test Suite: Cosine Similarity and Index Correctness",
  "summary": "Thorough unit and benchmark tests for the brute-force vector search implementation, covering cosine similarity edge cases, index CRUD semantics, ranked search correctness, JSON persistence, and performance at realistic scales. Acts as a contract specification for the VectorIndex API.",
  "concepts": [
    "cosine similarity",
    "VectorIndex",
    "brute-force search",
    "float32",
    "upsert",
    "JSON persistence",
    "ranked order",
    "TopK",
    "edge cases",
    "NaN prevention",
    "benchmark",
    "sentence-transformers",
    "384 dimensions"
  ],
  "categories": [
    "testing",
    "vector search",
    "benchmarks",
    "Go",
    "test"
  ],
  "source_docs": [
    "d2e414146e0b854c"
  ],
  "backlinks": null,
  "word_count": 586,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Vector similarity bugs are silent: a wrong cosine score does not throw an error, it just returns bad results. `vsearch_test.go` exists to pin the exact numerical behavior of `CosineSimilarity` and the correctness of all index operations before the vector layer is used in production search flows.

## Cosine Similarity Tests

Six tests cover the mathematical edge cases:

| Test | Scenario | Expected |
|---|---|---|
| `Identical` | Same vector | 1.0 (within 1e-6) |
| `Orthogonal` | `[1,0]` vs `[0,1]` | 0.0 |
| `Opposite` | `v` vs `-v` | -1.0 |
| `DifferentLengths` | Dimension mismatch | 0.0 (not panic) |
| `Empty` | nil vs nil | 0.0 (not panic) |
| `ZeroMagnitude` | All-zero vector | 0.0 (not NaN) |

The tolerance of 1e-6 is appropriate for float32 arithmetic. The zero-magnitude test is the most important safety test: dividing by a zero norm produces NaN in IEEE 754, and NaN comparisons are always false, which would cause the search ranker to mis-sort results in unpredictable ways.

## Index CRUD Tests

`TestVectorIndex_AddAndLen` verifies that a new index starts empty and `Len()` tracks insertions correctly.

`TestVectorIndex_AddOverwrite` tests the upsert semantic: adding the same ID twice must leave exactly one entry with the updated vector. Without this guarantee, repeated indexing of a document would cause the brute-force scan to score it multiple times, artificially inflating its rank.

`TestVectorIndex_Remove` verifies that a removed entry does not appear in subsequent searches and that the index length decreases. It also implicitly tests the swap-and-truncate removal — if the implementation incorrectly copies the slice, the removed slot might persist.

## Search Ranking Tests

`TestVectorIndex_Search_RankedOrder` inserts vectors at known angles from a query and verifies that results come back in descending cosine order. This is the core correctness test: a search that returns results in wrong order is worse than no search at all.

`TestVectorIndex_Search_TopK` verifies that a limit of K returns at most K results even when the index contains more entries.

`TestVectorIndex_Search_Empty` and `TestVectorIndex_Search_EmptyQuery` confirm that searching an empty index or with an all-zero query vector returns an empty slice rather than panicking.

## Persistence Tests

`TestVectorIndex_SaveLoad` writes an index to a temp file and reads it back, comparing IDs and vector values. Float32 values are compared with a small tolerance because JSON serialization of floats introduces rounding.

`TestLoadVectorIndex_MissingFile` confirms that loading from a nonexistent path returns an empty index, not an error. This matches the intended initialization behavior.

`TestLoadVectorIndex_BadJSON` confirms that a file containing invalid JSON returns an error rather than a silently empty or corrupted index.

## Benchmarks

Three benchmarks measure search throughput at realistic scales:

| Benchmark | Corpus | Dimensions | Purpose |
|---|---|---|---|
| `BenchmarkCosineSimilarity_128d` | N/A | 128 | Raw similarity cost |
| `BenchmarkVectorSearch_1k_128d` | 1,000 | 128 | Small-scale query latency |
| `BenchmarkVectorSearch_10k_384d` | 10,000 | 384 | Sentence-embedding scale |

128 dimensions matches compact embedding models. 384 dimensions matches sentence-transformers (e.g., `all-MiniLM-L6-v2`). The 10k × 384 benchmark is the most representative of real usage and establishes the latency budget before HNSW becomes necessary.

## Known Gaps

- No test covers concurrent access (the index is documented as not thread-safe, but there is no test that detects a data race if the thread-safety contract is accidentally violated).
- No benchmark covers the `Add` or `Remove` paths under load — only `Search` is benchmarked.
- Float32 JSON round-trip precision loss is noted in comments but not quantified by a test that measures degradation in similarity scores after persistence.