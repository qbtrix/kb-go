---
{
  "title": "kb-go Performance Benchmark Suite",
  "summary": "Provides Go benchmark functions covering all performance-sensitive operations in kb-go: tokenization, hashing, slugify, AST parsing for three languages, BM25 search, index rebuilding, directory scanning, and code context formatting. All benchmarks run offline with no API key required.",
  "concepts": [
    "benchmark",
    "BM25",
    "tokenize",
    "contentHash",
    "slugify",
    "AST parsing",
    "synthetic corpus",
    "deterministic seed",
    "benchmem",
    "index rebuild",
    "scanDir",
    "performance testing"
  ],
  "categories": [
    "testing",
    "performance",
    "benchmarks",
    "Go",
    "test"
  ],
  "source_docs": [
    "553f4f06bd1c33ee"
  ],
  "backlinks": null,
  "word_count": 482,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Purpose

Performance regressions in a knowledge base tool are subtle — a 2x slowdown in tokenization compounds across thousands of articles. `kb_bench_test.go` exists to catch these regressions before they ship. Every function that runs in a hot loop during `build`, `search`, or `watch` has a corresponding benchmark.

## Benchmark Coverage

| Benchmark | What it measures |
|---|---|
| `BenchmarkTokenize` | Token extraction speed (core of BM25 indexing) |
| `BenchmarkContentHash` | SHA-256 throughput for cache invalidation |
| `BenchmarkSlugify` | URL-safe ID generation from titles |
| `BenchmarkParseGo` | Go AST parse time via `go/ast` |
| `BenchmarkParsePython` | Regex-based Python parser speed |
| `BenchmarkParseTypeScript` | Regex-based TypeScript parser speed |
| `BenchmarkBM25Search` | Query latency over a synthetic 1000-article corpus |
| `BenchmarkRebuildIndex` | Full index reconstruction time |
| `BenchmarkScanDir` | File system walk performance |
| `BenchmarkFormatCodeContext` | LLM prompt generation speed |

## Synthetic Corpus Generation

`generateCorpus(n)` creates `n` `WikiArticle` values using a fixed seed (`rand.NewSource(42)`). The fixed seed is deliberate: benchmarks must be deterministic across runs so that `benchstat` comparisons are meaningful. A random seed would introduce variance that masks real performance changes.

The generated articles use a vocabulary drawn from software engineering terms (`authentication`, `database`, `middleware`, `async`, `distributed`, etc.) and connective words. Word counts vary between 50 and 200, approximating the distribution of real articles. Concepts are randomly assigned 2–3 items, reflecting typical tagging density.

## File Loading Helpers

`loadExampleFile` and `findExamplesDir` locate real source files from the `examples/` directory relative to the module root. The search tries both `.` and `..` as base paths, which handles the case where benchmarks are run from a subdirectory. This fallback pattern prevents a common benchmark failure: the test binary changes its working directory depending on how `go test` is invoked.

## Running the Benchmarks

```
go test -bench=. -benchmem
```

`-benchmem` enables allocation reporting, which is often more diagnostic than raw time. A tokenizer that allocates one slice per word is slower than one that pre-allocates, but the timing difference may be small — allocation count reveals the real problem.

## Design Choices

All benchmarks are fully offline. This is a hard constraint: benchmarks that require an API key cannot run in CI without credential management. The benchmarks validate that the computational core (parsing, hashing, search) meets latency targets independently of LLM call overhead.

The `BenchmarkBM25Search` benchmark runs over a 1000-article corpus, which is representative of a medium-sized knowledge base. The index rebuild benchmark complements this by measuring the cost of the initial indexing pass that happens on every `build` run.

## Known Gaps

- There is no benchmark for `compileLLM` or `lintLLM` (these require API access and are intentionally excluded).
- `BenchmarkScanDir` scans the actual filesystem; performance will vary significantly between machines with fast SSDs and slower network filesystems.
- No benchmark covers the `watch` command's fsnotify event loop or the `changedFilesSinceRef` git invocation.