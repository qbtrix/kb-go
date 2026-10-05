---
{
  "title": "Brute-Force Vector Search Index with Cosine Similarity",
  "summary": "Implements a flat in-memory vector index backed by brute-force cosine similarity search, designed as the dense retrieval layer (level 2) in kb-go's three-tier search stack. Suitable for corpora under ~100k vectors with JSON persistence and no external dependencies.",
  "concepts": [
    "vector search",
    "cosine similarity",
    "flat index",
    "brute-force",
    "float32",
    "VectorIndex",
    "VectorEntry",
    "JSON persistence",
    "dense retrieval",
    "HNSW",
    "embedding",
    "thread safety",
    "upsert"
  ],
  "categories": [
    "search",
    "vector indexing",
    "retrieval",
    "Go"
  ],
  "source_docs": [
    "b3abf763c94a7997"
  ],
  "backlinks": null,
  "word_count": 587,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Role in the Retrieval Stack

kb-go uses three retrieval layers:

- **Level 0** — Concept graph lookup (exact entity match)
- **Level 1** — BM25 keyword search
- **Level 2** — Dense vector search (this file)

The vector layer handles queries where keyword matching fails: paraphrases, semantic similarity, cross-lingual queries. It is intentionally decoupled from embedding generation — kb-go does not call an embedding API itself. The caller embeds text with whatever model fits their budget and passes `[]float32` vectors to the index.

## VectorIndex

```go
type VectorIndex struct {
    Entries []VectorEntry `json:"entries"`
}

type VectorEntry struct {
    ID     string    `json:"id"`
    Vector []float32 `json:"vector"`
}
```

The index is a plain slice. This is deliberate for the target scale: under 100k vectors, a brute-force scan over a flat slice is cache-friendly and avoids the complexity of HNSW graph maintenance. At 128 dimensions, 100k vectors occupy roughly 51 MB — comfortably in memory.

## Core Operations

**Add** — Overwrites an existing entry if the ID already exists, otherwise appends. The overwrite scan is O(n) but acceptable at the intended scale. The upsert semantics prevent duplicate IDs from accumulating if the caller re-indexes a document.

**Remove** — Finds the entry by ID and removes it by swapping with the last element and truncating the slice. Swap-and-truncate preserves O(1) removal without allocating a new backing array, unlike `append(s[:i], s[i+1:]...)` which copies the tail.

**Search** — Scores every entry against the query vector using cosine similarity, then returns the top K results sorted by descending score. The full scan is O(n·d) where d is the vector dimension. For the target scale this completes in sub-millisecond time.

## CosineSimilarity

```go
func CosineSimilarity(a, b []float32) float32
```

Returns 0 for nil slices, zero-magnitude vectors, or mismatched dimensions rather than panicking or returning NaN. Each of these guards prevents a silent correctness bug:

- **Nil guard** — a document with no vector should rank last, not crash the search.
- **Zero-magnitude guard** — division by zero would produce NaN, which sorts unpredictably.
- **Dimension mismatch guard** — embedding models with different output sizes must not be mixed silently; returning 0 forces the caller to notice mismatches through poor recall.

## Persistence

`Save` marshals the entire index to JSON at the given path. `LoadVectorIndex` reads it back, returning an empty index (not an error) if the file does not exist. The missing-file behavior is intentional: on first run, no index file exists yet, and the caller should get a valid empty index rather than an initialization error.

JSON persistence trades storage efficiency for simplicity. A 100k × 128-dimension float32 index serialized as JSON is large (~200 MB text), but it requires no binary format versioning or custom reader.

## Thread Safety

The index is explicitly documented as not thread-safe. Concurrent reads are safe (no writes), but concurrent `Add`/`Remove` calls must be serialized by the caller. In CLI usage this is not an issue; in a server context the caller would wrap with a mutex.

## Known Gaps

- The `zvec` build tag for HNSW-scale search is referenced in comments but not implemented. At 1M+ vectors, brute-force becomes too slow.
- JSON persistence of float32 arrays loses precision (JSON floats are 64-bit but written as decimal strings). For high-dimensional vectors this can degrade similarity accuracy slightly.
- No batch insert path: ingesting 100k vectors requires 100k individual `Add` calls, each scanning the existing entries for duplicates.
- The index has no version field in the JSON, so schema changes would silently corrupt existing index files.