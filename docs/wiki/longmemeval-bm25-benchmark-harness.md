---
{
  "title": "LongMemEval BM25 Benchmark Harness",
  "summary": "Implements the full LongMemEval evaluation harness for kb-go's BM25 and entity-boosted search, running recall-at-K measurements over a 500-question benchmark dataset of conversational memory retrieval tasks. Provides oracle, small-sample, and export variants to support both rapid iteration and cross-language comparison.",
  "concepts": [
    "LongMemEval",
    "BM25",
    "entity boosting",
    "recall at K",
    "session retrieval",
    "benchmark harness",
    "rankerFunc",
    "buildSessionCorpus",
    "LMETurn",
    "export rankings",
    "user turns",
    "conversational memory"
  ],
  "categories": [
    "benchmarks",
    "retrieval evaluation",
    "search quality",
    "testing",
    "test"
  ],
  "source_docs": [
    "a7aac07a6a385fe2"
  ],
  "backlinks": null,
  "word_count": 547,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## What LongMemEval Tests

LongMemEval is a published benchmark for long-term memory retrieval: given a question about a past conversation, retrieve the correct session from a haystack of up to hundreds of sessions. It specifically stresses retrieval systems that must match questions phrased differently from stored content.

kb-go uses this benchmark to validate that its BM25 engine, combined with entity extraction boosting, is competitive for the session-retrieval use case before adding heavier retrieval layers.

## Data Structures

```go
type LMEQuestion struct {
    QuestionID        string
    QuestionType      string        // taxonomy: single-session, multi-session, temporal, etc.
    Question          string
    HaystackSessionIDs []string
    HaystackSessions  [][]LMETurn   // full conversation turns
    AnswerSessionIDs  []string      // ground truth
}

type LMETurn struct {
    Role    string  // "user" or "assistant"
    Content string
}
```

Each question comes with a haystack of sessions and a ground-truth set of answer session IDs. The benchmark checks whether the retrieval system surfaces any answer session in the top K results (`recallAtK`).

## Retrieval Pipeline

`buildSessionCorpus` concatenates only user turns from each session into a single document per session. Assistant turns are excluded — they often echo the user's words and inflate match scores for irrelevant sessions. This reduces noise in BM25 term frequency counts.

Two rankers are tested:

- `bm25RankSessions` — pure BM25 keyword scoring
- `entityBoostedRank` — BM25 plus entity extraction: named entities in the question are detected, sessions mentioning the same entities get a score boost

`rankerFunc` is a type alias for the ranking function signature, allowing `runBenchmark` to accept either ranker without code duplication.

## Test Variants

| Test | Purpose |
|---|---|
| `TestLongMemEval_BM25_Oracle` | Full 500-question run, pure BM25 |
| `TestLongMemEval_EntityBoosted_Oracle` | Full 500-question run, entity boosting |
| `TestLongMemEval_BM25_Small` | 50-question sample for rapid CI feedback |
| `TestLongMemEval_EntityBoosted_Small` | 50-question sample, entity boosting |
| `TestLongMemEval_ExportRankings` | Exports top-15 per question as JSON for Python pipeline |

The oracle variants run the full benchmark and report R@1, R@5, and R@10. The small variants exist for quick sanity checks — running all 500 questions takes several minutes.

## Export for Cross-Language Comparison

`TestLongMemEval_ExportRankings` writes BM25 top-15 results per question to a JSON file. A Python script can then apply dense embedding reranking on top of these candidates, combining Go's fast BM25 with Python's embedding ecosystem. This two-stage pipeline avoids re-implementing embedding models in Go.

## Skip Behavior

All tests call `t.Skipf` if the data file is missing. The 265 MB dataset is not committed to the repository; developers download it separately. CI passes without the dataset.

## Recall Metric

`recallAtK` returns true if any answer session ID appears in the top K ranked results. This is a lenient metric: a single correct session in position K counts as a hit. The benchmark reports R@1, R@5, and R@10 to show the full retrieval curve.

## Known Gaps

- The dataset path (`lmeDataPath`) is a hardcoded constant defined outside this file; there is no `--dataset` flag to override it at runtime.
- `buildSessionCorpus` excludes assistant turns unconditionally. For some question types (e.g., questions about what the assistant said), this hurts recall.
- Entity extraction in `entityBoostedRank` is not documented in this file; its implementation quality directly determines the boost's effectiveness.
- No statistical significance test is run — two recall numbers are compared visually, not with confidence intervals.