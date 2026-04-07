# kb — Headless Knowledge Base Engine

Single-binary CLI that turns codebases into searchable, LLM-compiled knowledge wikis.

No embeddings. No vectors. No database. No Python runtime. One 6MB binary.

The LLM understands your code **at write time**. BM25 searches it **at read time** in 10ms.

## The Problem

Every time an AI agent reads your codebase, it burns thousands of tokens re-reading the same files. RAG pipelines add vector databases, embedding models, and chunking strategies — all to approximate understanding.

`kb` takes a different approach: **compile once, search forever.**

```
Your codebase (129 files) → kb build → 129 structured articles + concept graph
                                        ↓
                              Search in 10ms, not 10 seconds
                              Zero tokens burned on re-reads
                              Works offline after first build
```

## How It's Different

| | kb | Traditional RAG | Codebase indexers |
|---|---|---|---|
| **Understanding** | LLM compiles at write time | Embedding similarity at query time | Keyword/AST only |
| **Search** | BM25 with concept boosting (~10ms) | Vector similarity + reranking (~200ms) | Keyword match |
| **Storage** | Flat markdown files | Vector database (Pinecone, Chroma, etc.) | Database/index files |
| **Dependencies** | One 6MB binary | Python + embedding model + vector DB | Language-specific tooling |
| **Incremental** | SHA256 cache, skip unchanged files | Re-embed everything | Varies |
| **Offline** | After first build, fully offline | Needs embedding model running | Usually offline |
| **Output** | Human-readable wiki articles | Opaque vectors | Code navigation |

## Performance

Tested on real, production codebases using `claude-haiku-4-5-20251001`:

### Build Speed

| Codebase | Language | Files | Cold Build | Per File | Warm Build (cache) |
|----------|----------|-------|-----------|---------|-------------------|
| **litestream** | Go | 129 | 355s | 2.75s | **0.03s** |
| **flask** | Python | 83 | 78s | 3.25s | **0.01s** |
| Small corpus | Go/Py/TS | 10 | 9.5s | 1.90s | **0.01s** |

Cold builds run 5 files in parallel. Warm builds are **instant** — content-hash cache skips unchanged files.

### Search Quality

| Corpus | Queries Tested | Relevance Score | Avg Latency |
|--------|---------------|----------------|------------|
| Go (5 articles) | 6 | **100%** (6/6) | 9.8ms |
| Python (3 articles) | 4 | **100%** (4/4) | 10.0ms |
| TypeScript (2 articles) | 2 | **100%** (2/2) | 10.0ms |
| litestream (129 articles) | 5 | **100%** (5/5) | ~11ms |
| flask (24 articles) | 5 | **100%** (5/5) | ~11ms |

100% search relevance across 22 tested queries — the right article surfaces first every time.

BM25 with title boosting (3x) and concept boosting (2x) outperforms naive keyword search.

### Raw Throughput (Apple M2 Pro, offline)

| Operation | Speed | Notes |
|-----------|-------|-------|
| Go AST parse | **17,586 files/sec** | Full `go/ast` stdlib parser |
| Python parse | **4,783 files/sec** | Regex-based extraction |
| TypeScript parse | **6,136 files/sec** | Regex-based extraction |
| BM25 search (1K articles) | **11ms** | Pre-tokenized index |
| BM25 search (5K articles) | **55ms** | Scales linearly |
| Index rebuild (1K articles) | **0.8ms** | Concept graph + backlinks |
| Content hash (10KB file) | **6 microseconds** | SHA256 |
| Slugify | **500K ops/sec** | URL-safe slug generation |

### What You Get Per Codebase

| Metric | litestream (129 files) | flask (83 files) |
|--------|----------------------|-------------------|
| Articles | 129 | 24 |
| Total words | 74,931 | 13,893 |
| Avg words/article | 580 | 578 |
| Concepts extracted | 990 | 263 |
| Avg concepts/article | 7.6 | 10.9 |
| Categories | 353 | 112 |

Each article is a self-contained, structured document with title, summary, concepts, categories, and backlinks to related articles. Not chunks — complete knowledge.

## Install

```bash
# Download binary (macOS ARM)
curl -L https://github.com/qbtrix/kb-go/releases/download/v0.1.0/kb-darwin-arm64 -o kb && chmod +x kb

# Or build from source
go install github.com/qbtrix/kb-go@latest

# Or clone and build
git clone https://github.com/qbtrix/kb-go && cd kb-go && go build -o kb .
```

**As a Claude Code skill:**
```bash
npx skills add qbtrix/kb-go
```

Set your API key: `export ANTHROPIC_API_KEY="sk-..."`

## Quick Start

```bash
# Build a wiki from your Go codebase
kb build ./src --scope myapp --pattern "*.go"

# Multi-language in one pass
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Search it (10ms)
kb search "auth middleware" --scope myapp

# Second build skips unchanged files (0.01s)
kb build ./src --scope myapp --pattern "*.go"

# Export as readable markdown
kb build ./src --scope myapp --pattern "*.go" --output docs/wiki/

# Watch for changes, auto-rebuild
kb watch ./src --scope myapp --pattern "*.go"
```

## Commands

| Command | What it does |
|---------|-------------|
| `kb build <path>` | Scan files, parse AST, compile with LLM, build wiki |
| `kb search <query>` | BM25 search over compiled articles |
| `kb ingest [file]` | Ingest a file or piped stdin text |
| `kb show <id>` | Display a full article |
| `kb list` | List all articles |
| `kb stats` | Article/concept/word counts |
| `kb lint` | Structural health check (`--llm` for deep check) |
| `kb recompile <id>` | Force recompile from raw source (`--all` for everything) |
| `kb watch <path>` | Auto-rebuild on file changes |
| `kb clear` | Delete all data for a scope |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scope` | `default` | Knowledge scope (multi-tenant) |
| `--json` | off | Machine-readable JSON output |
| `--model` | `claude-haiku-4-5-20251001` | LLM model for compilation |
| `--pattern` | `*.py` | File patterns, comma-separated |
| `--concurrency` | `5` | Parallel LLM compilations |
| `--output` | — | Export wiki to directory |
| `--lang` | auto | Language hint for stdin ingest |

## How It Works

```
Source files
    ↓
AST parse (Go: go/ast, Python: regex, TypeScript: regex)
    ↓
LLM compile (Anthropic API, 5 concurrent)
    ↓
Wiki articles (markdown + JSON frontmatter)
    ↓
BM25 index (pre-tokenized, title/concept boosted)
    ↓
~/.knowledge-base/{scope}/
├── raw/       (original source, immutable)
├── wiki/      (compiled articles)
├── cache/     (SHA256 hashes + search index)
└── index.json (concept graph, backlinks, categories)
```

Every article is a readable `.md` file. No database, no binary formats. `cat` any article.

## Architecture

**One file: `kb.go` (2,463 lines). One dependency: `fsnotify`.**

| Component | Lines | What it does |
|-----------|-------|-------------|
| Data models | ~100 | RawDoc, WikiArticle, Concept, KnowledgeIndex, Cache |
| AST parsers | ~400 | Go (stdlib), Python (regex), TypeScript (regex) |
| Storage | ~200 | File-based CRUD, markdown + JSON frontmatter |
| BM25 search | ~100 | Title/concept-boosted BM25 with pre-tokenized index |
| LLM compilation | ~100 | Direct HTTP to Anthropic API, zero SDK deps |
| Lint | ~150 | Structural (instant) + LLM-powered (deep) |
| CLI | ~400 | All commands, flag parsing, JSON output |
| Watch mode | ~80 | fsnotify + 3s debounce |

## Testing

**37 unit tests + 10 performance benchmarks.** Zero external test deps.

```bash
go test -v ./...          # Unit tests (37 — all passing)
go test -bench=. ./...    # Performance benchmarks (10)
./bench.sh small          # Integration benchmarks (needs API key)
```

### Test Coverage

| Area | Tests | What's tested |
|------|-------|-------------|
| Storage | 6 | Article/rawdoc/index round-trip, frontmatter parsing (valid, empty, corrupt) |
| Search | 5 | BM25 ranking, empty query, empty corpus, result limits, no-match |
| Cache | 3 | Hit/miss detection, persistence round-trip |
| AST parsers | 6 | Go structs/interfaces/methods, Python classes/async/docstrings, TS interfaces/enums/arrow functions |
| Lint | 3 | Empty KB, missing concepts, broken backlinks |
| File scanning | 3 | Pattern matching, directory skipping (.git, node_modules), multi-pattern |
| Helpers | 8 | Slugify, tokenize, content hash, word count, truncate |
| Compat | 1 | Python knowledge-base format interop |
| **Performance** | **10** | Tokenize (100/1K/10K words), hash (1K/10K/100KB), AST parse (3 langs), BM25 (10/100/1K/5K articles), index rebuild, scan, format |

### Run Benchmarks Yourself

```bash
# Offline benchmarks (no API key needed)
go test -bench=. -benchmem

# Full pipeline on example codebases
./bench.sh small                              # 10 files, ~30s
./examples/fetch.sh all && ./bench.sh medium  # 129 files, ~6 min
./bench.sh all                                # Everything

# Results saved to bench_results.json
cat bench_results.json
```

## Examples

```
examples/
├── small/          # 10 hand-written files (Go/Python/TS task management API)
├── golden/         # Expected search results + concept extraction
├── output/         # Pre-built wiki articles (reference, no API key needed)
├── fetch.sh        # Download litestream (Go, 129 files) + flask (Python, 83 files)
└── README.md
```

The `examples/output/` directory contains pre-built wikis you can search immediately without an API key.

## License

MIT
