# LongMemEval Benchmark

kb's BM25 search scored **95.0% R@5** on the [LongMemEval](https://github.com/xiaowu0162/LongMemEval) benchmark (ICLR 2025) with zero embeddings, zero vector DBs, and zero ML dependencies. 500 questions in 0.5 seconds.

## Reproduce

### 1. Download the dataset

```bash
curl -L "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json" \
  -o benchmarks/longmemeval/longmemeval_s_cleaned.json

curl -L "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_oracle.json" \
  -o benchmarks/longmemeval/longmemeval_oracle.json
```

### 2. Run the Go benchmark

```bash
# BM25 on full haystack (53 sessions per question)
go test -v -run TestLongMemEval_BM25_Small -timeout 120s .

# BM25 with entity boosting
go test -v -run TestLongMemEval_EntityBoosted_Small -timeout 120s .

# Error analysis (shows which questions are missed and why)
go test -v -run TestLongMemEval_ErrorAnalysis -timeout 120s .

# Oracle (evidence-only sessions, should be ~100%)
go test -v -run TestLongMemEval_BM25_Oracle -timeout 120s .
```

### 3. Run the Python benchmark (optional, for tier 2/3)

```bash
cd benchmarks/longmemeval

# Tier 1: BM25 only (matches Go results)
uv run python bench.py --tier 1

# Tier 2: BM25 + soul-protocol LLM rerank (needs ANTHROPIC_API_KEY)
uv run python bench.py --tier 2 --engine haiku

# Tier 3: BM25 + zvec dense search (needs sentence-transformers)
uv run python bench.py --tier 3
```

## Results

| Method | R@1 | R@5 | R@10 | Time | Dependencies |
|--------|-----|-----|------|------|-------------|
| kb BM25 | 82.8% | 94.8% | 96.6% | 0.5s | zero |
| kb BM25 + entity boost | 85.0% | 95.0% | 96.6% | 0.5s | zero |
| MemPalace (raw mode) | — | 96.6% | — | minutes | ChromaDB + ONNX + ST |

## Files

- `longmemeval_s_cleaned.json` — full dataset, 265MB (gitignored, download above)
- `longmemeval_oracle.json` — oracle dataset, 15MB (gitignored)
- `bench.py` — Python benchmark harness for multi-tier evaluation
- `tweet-card.html` — visual card for sharing results
