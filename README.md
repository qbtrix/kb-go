[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/qbtrix/kb-go?color=blue)](https://github.com/qbtrix/kb-go/releases)
[![Tests](https://img.shields.io/badge/tests-37%20passing-brightgreen)](#testing)
[![Benchmarks](https://img.shields.io/badge/benchmarks-10-blue)](#raw-throughput)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Skills.sh](https://img.shields.io/badge/skills.sh-kb-purple)](https://skills.sh)

# kb — Headless Knowledge Base Engine

Single-binary CLI that turns codebases into searchable, LLM-compiled knowledge wikis.

No embeddings. No vectors. No database. No Python runtime. **One 6MB binary.**

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

## How It Compares

### kb vs RAG vs Knowledge Graphs

| | **kb** | **Traditional RAG** | **Graphify** (knowledge graph) |
|---|---|---|---|
| **Approach** | LLM compiles at write time | Embedding similarity at query time | Knowledge graph from AST + LLM |
| **Search** | BM25 + concept boost (~10ms) | Vector similarity + reranking (~200ms) | Graph traversal + BFS/DFS |
| **Binary** | **6MB** Go binary, zero deps | Python + embedding model + vector DB | Python + NetworkX + tree-sitter + vis.js |
| **Languages** | Go, Python, TypeScript/JS | Language-agnostic (embeds raw text) | 14 languages via tree-sitter |
| **Output** | Readable wiki articles (markdown) | Opaque vector embeddings | Interactive graph (HTML/Obsidian/Neo4j) |
| **Incremental** | SHA256 cache, 0.03s warm build | Re-embed everything | SHA256 cache, per-file |
| **Offline** | Fully offline after build | Needs embedding model | Needs Claude for docs |
| **Install** | `go install` or download binary | `pip install` + infra setup | `pip install graphifyy` |
| **Token savings** | Compile once, never re-read | N/A (embeds, not reads) | 71.5x claimed on mixed corpus |
| **Search relevance** | **100%** (22/22 queries) | Depends on embedding quality | N/A (graph queries, not search) |

### Where kb wins
- **Speed**: 10ms search, 0.01s warm builds, 17K files/sec AST parsing
- **Simplicity**: one binary, no runtime deps, no database, no infra
- **Predictability**: 100% search relevance on tested queries — the right article surfaces first
- **Portability**: markdown files you can `cat`, `grep`, commit to git

### Where others win
- **Graphify**: richer relationship modeling (edges between concepts, not just articles), visual exploration, 14 language AST support, image/PDF ingestion via Claude vision
- **RAG**: better for ad-hoc questions over massive unstructured text, semantic similarity for vague queries

`kb` is built for **codebase understanding** — structured, fast, predictable. If you need a knowledge graph or vector search, those tools exist. If you want a wiki that actually works, use `kb`.

## Performance

Tested on real, production codebases using `claude-haiku-4-5-20251001`:

### Build Speed

| Codebase | Language | Files | Cold Build | Per File | Warm Build |
|----------|----------|-------|-----------|---------|-----------|
| [**litestream**](https://github.com/benbjohnson/litestream) | Go | 129 | 355s | 2.75s | **0.03s** |
| [**flask**](https://github.com/pallets/flask) | Python | 83 | 78s | 3.25s | **0.01s** |
| [Small corpus](examples/small/) | Go/Py/TS | 10 | 9.5s | 1.90s | **0.01s** |

Cold builds run **5 files in parallel**. Warm builds are instant — SHA256 content-hash cache skips unchanged files.

### Search Relevance

| Corpus | Queries Tested | Correct Top Result | Avg Latency |
|--------|---------------|-------------------|------------|
| [Go examples](examples/small/go/) | 6 | **100%** (6/6) | 9.8ms |
| [Python examples](examples/small/python/) | 4 | **100%** (4/4) | 10.0ms |
| [TypeScript examples](examples/small/typescript/) | 2 | **100%** (2/2) | 10.0ms |
| litestream (129 articles) | 5 | **100%** (5/5) | ~11ms |
| flask (24 articles) | 5 | **100%** (5/5) | ~11ms |

**22/22 queries** return the correct article as the top result. BM25 with title boosting (3x) and concept boosting (2x).

Golden test queries: [`examples/golden/search_relevance.json`](examples/golden/search_relevance.json)

### Raw Throughput

Offline benchmarks on Apple M2 Pro — no API key needed:

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
| Articles generated | 129 | 24 |
| Total words | 74,931 | 13,893 |
| Avg words/article | 580 | 578 |
| Concepts extracted | 990 | 263 |
| Avg concepts/article | 7.6 | 10.9 |
| Categories | 353 | 112 |

Each article is a self-contained document with title, summary, full content, concepts, categories, and backlinks. Not chunks — **complete knowledge**.

Pre-built reference wikis: [`examples/output/`](examples/output/)

## Install

```bash
# Download binary (macOS ARM)
curl -L https://github.com/qbtrix/kb-go/releases/download/v0.1.0/kb-darwin-arm64 -o kb && chmod +x kb

# Or build from source
go install github.com/qbtrix/kb-go@latest

# Or clone and build
git clone https://github.com/qbtrix/kb-go && cd kb-go && go build -o kb .
```

**As a Claude Code / AI agent skill:**
```bash
npx skills add qbtrix/kb-go
```

**Binaries available:** macOS (ARM/x86), Linux (ARM/x86) — see [Releases](https://github.com/qbtrix/kb-go/releases)

Set your API key: `export ANTHROPIC_API_KEY="sk-..."`

## Quick Start

```bash
# Build a wiki from your codebase
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Search it (10ms)
kb search "auth middleware" --scope myapp

# Second build is instant (cache)
kb build ./src --scope myapp --pattern "*.go"

# Export as readable markdown wiki
kb build ./src --scope myapp --output docs/wiki/

# Watch for changes, auto-rebuild
kb watch ./src --scope myapp --pattern "*.go"

# Pipe extracted text from external tools
pdftotext paper.pdf - | kb ingest --scope myapp --source "paper.pdf"
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

## AST Parsing

Code files get AST-parsed before LLM compilation. The LLM sees both raw source **and** extracted structure — better articles, fewer wasted tokens.

| Language | Parser | What it extracts |
|----------|--------|-----------------|
| Go | `go/parser` (stdlib) | packages, structs, interfaces, methods, functions, constants |
| Python | regex | classes, methods (async), imports, docstrings, constants |
| TypeScript/JS | regex | classes, interfaces, enums, types, functions, arrow functions |

## How It Works

```
Source files
    ↓
AST parse (Go: go/ast, Python: regex, TypeScript: regex)
    ↓
LLM compile (Anthropic API, 5 concurrent, token usage tracked)
    ↓
Wiki articles (markdown + JSON frontmatter)
    ↓
BM25 index (pre-tokenized, title/concept boosted)
    ↓
~/.knowledge-base/{scope}/
├── raw/       (original source, immutable)
├── wiki/      (compiled articles)
├── cache/     (SHA256 hashes + search tokens)
└── index.json (concept graph, backlinks, categories)
```

Every article is a readable `.md` file. No database, no binary formats. `cat` any article.

## Architecture

**One file: [`kb.go`](kb.go) (2,463 lines). One dependency: `fsnotify`.**

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

| Area | Tests | What's verified |
|------|-------|----------------|
| Storage | 6 | Article/rawdoc/index round-trip, frontmatter parsing |
| Search | 5 | BM25 ranking, edge cases, limits |
| Cache | 3 | Hit/miss detection, persistence |
| AST parsers | 6 | Go/Python/TypeScript extraction |
| Lint | 3 | Empty KB, missing concepts, broken backlinks |
| File scanning | 3 | Patterns, directory skipping, multi-pattern |
| Helpers | 8 | Slugify, tokenize, hash, word count |
| Compat | 1 | Python knowledge-base format interop |
| **Benchmarks** | **10** | All core operations across scale tiers |

### Run Benchmarks Yourself

```bash
# Offline (no API key needed)
go test -bench=. -benchmem

# Full pipeline on real codebases
./bench.sh small                              # 10 files, ~30s
./examples/fetch.sh all && ./bench.sh medium  # 129 files, ~6 min
./bench.sh all                                # Everything

# Results saved to bench_results.json
```

## Examples

```
examples/
├── small/              10 hand-written files (Go/Python/TS task API)
│   ├── go/             server, handler, middleware, models, config
│   ├── python/         service, models, utils
│   └── typescript/     api, types
├── golden/             Expected search results + concept extraction
│   ├── search_relevance.json
│   └── concept_expected.json
├── output/             Pre-built wiki articles (no API key needed to browse)
│   ├── small-go/
│   ├── small-python/
│   └── small-typescript/
├── fetch.sh            Download litestream (Go) + flask (Python)
└── README.md
```

Browse the [example source files](examples/small/) or the [pre-built wiki output](examples/output/).

## License

MIT
