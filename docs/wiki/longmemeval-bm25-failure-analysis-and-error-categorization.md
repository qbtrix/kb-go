---
{
  "title": "LongMemEval BM25 Failure Analysis and Error Categorization",
  "summary": "A diagnostic test that runs the LongMemEval 500-question benchmark, identifies every question where BM25 fails to retrieve the correct session in the top 5 results, and categorizes misses by question type and retrieval gap. The output guides decisions about whether to add dense embedding search or LLM reranking.",
  "concepts": [
    "LongMemEval",
    "BM25",
    "recall at K",
    "entity boosting",
    "dense search",
    "LLM reranking",
    "error analysis",
    "MissedQuestion",
    "R@5",
    "R@10",
    "question type",
    "retrieval gap",
    "semantic search"
  ],
  "categories": [
    "benchmarks",
    "retrieval evaluation",
    "search quality",
    "testing",
    "test"
  ],
  "source_docs": [
    "e2160e21f4112e46"
  ],
  "backlinks": null,
  "word_count": 528,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

BM25 is a keyword-matching algorithm. It fails on questions phrased differently from the source material — paraphrases, entity mentions, or temporal references. `TestLongMemEval_ErrorAnalysis` exists to quantify exactly where BM25 fails and distinguish two failure modes with very different remediation costs:

1. **Reranking gap** — the correct session is in the top 10 but not top 5. An LLM reranker can promote it cheaply.
2. **Dense search gap** — the correct session is not in the top 10 at all. Only semantic (embedding-based) retrieval can find it.

This distinction matters because LLM reranking costs API calls per query but requires no infrastructure change, while dense embedding search requires embedding all sessions upfront and running a vector index.

## MissedQuestion Structure

```go
type MissedQuestion struct {
    ID          string
    Type        string   // e.g. "single-session-user", "multi-session"
    Question    string
    TopRanked   string   // session ID BM25 ranked #1
    CorrectIDs  []string // ground-truth session IDs
    CorrectRank int      // actual rank of correct session, -1 if absent
    InTop10     bool
}
```

`CorrectRank = -1` signals the correct session was not found anywhere in the ranked list — the hardest failure class. `InTop10` separates the reranking-fixable cases from the ones that need dense search.

## Test Flow

The test iterates over all 500 questions. For each question:

1. Build a session corpus from the haystack sessions.
2. Run `entityBoostedRank` (BM25 + entity extraction boost).
3. Check recall at k=5 and k=10.
4. If R@5 misses, record a `MissedQuestion` with rank diagnostics.

At the end, the test logs three counts: R@5 hits, R@10 hits, and the gap between them. It also breaks misses down by `QuestionType` (the LongMemEval taxonomy includes `single-session-user`, `multi-session`, `temporal-reasoning`, etc.), so developers can see whether failures cluster in a particular category.

## Skip Behavior

If the data file is missing, the test calls `t.Skipf` rather than `t.Fatalf`. This is intentional: the 265 MB LongMemEval dataset is not committed to the repository. The test is designed to run locally when the developer downloads the dataset, not in CI. A fatal failure would block the entire test suite.

## Output Format

Results are logged via `t.Logf`, not written to a file. To persist results for analysis, the developer runs `go test -v -run TestLongMemEval_ErrorAnalysis 2>&1 | tee errors.log`. The `MissedQuestion` struct can be marshaled to JSON (all fields have `json:` tags) but the current implementation only logs summary statistics.

## Diagnostic Value

The R@10-but-not-R@5 count directly answers: "how many questions would a reranker fix?" The "not in top 10" count answers: "how many questions require dense search?" These two numbers set the expected ROI for each retrieval upgrade before any code is written.

## Known Gaps

- Misses are only logged, not written to a JSON file for automated tracking across runs. There is no baseline comparison — a developer cannot tell if a code change improved or worsened the miss rate without manually comparing log output.
- The `MissedQuestion` JSON tags are defined but the struct is never actually marshaled in this file; the serialization path is unused.
- Only `entityBoostedRank` is analyzed; `bm25RankSessions` (plain BM25) is not compared side-by-side in this diagnostic, making it hard to attribute improvements to entity boosting specifically.