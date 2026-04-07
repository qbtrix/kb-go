# Examples & Benchmarks

Test corpora for benchmarking kb-go across languages and scales.

## Corpora

| Tier | Files | Languages | Source | Included |
|------|-------|-----------|--------|----------|
| **small** | 10 | Go, Python, TypeScript | Hand-written task API | Yes (in-repo) |
| **medium** | ~60 | Go | [litestream](https://github.com/benbjohnson/litestream) v0.4.0 | Downloaded via `fetch.sh` |
| **large** | ~200 | Python | [flask](https://github.com/pallets/flask) 3.1.0 | Downloaded via `fetch.sh` |

## Quick Start

```bash
# Build the kb binary
cd .. && go build -o kb .

# Run on small corpus (bundled, no download needed)
./kb build examples/small/go --scope small-go --pattern "*.go"
./kb build examples/small/python --scope small-python --pattern "*.py"
./kb build examples/small/typescript --scope small-ts --pattern "*.ts"

# Search
./kb search "authentication middleware" --scope small-go
./kb search "async task service" --scope small-python

# Download medium + large corpora
./examples/fetch.sh all

# Benchmark medium
./kb build examples/medium/litestream --scope bench-litestream --pattern "*.go"

# Benchmark large
./kb build examples/large/flask --scope bench-flask --pattern "*.py"
```

## Benchmarks

### Offline (no API key needed)

```bash
go test -bench=. -benchmem
```

Measures: tokenize, content hash, AST parsing (Go/Python/TS), BM25 search, index rebuild, file scanning.

### Full Pipeline (needs ANTHROPIC_API_KEY)

```bash
./bench.sh small       # Quick — 10 files, ~1 min
./bench.sh medium      # Moderate — ~60 files, ~5 min
./bench.sh large       # Stress — ~200 files, ~20 min
./bench.sh all         # Everything
```

Outputs `bench_results.json` with: build times, cache hit rates, search latency, quality metrics.

## Golden Files

`golden/search_relevance.json` — expected top search results per query.
`golden/concept_expected.json` — expected concepts per source file.

Used by `bench.sh` to measure search relevance and concept extraction quality.

## Clean Up

```bash
./examples/fetch.sh clean     # Remove downloaded corpora
./kb clear --scope small-go   # Remove KB data
```
