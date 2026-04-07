[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/qbtrix/kb-go?color=blue)](https://github.com/qbtrix/kb-go/releases)
[![Tests](https://img.shields.io/badge/tests-37%20passing-brightgreen)](#testing)
[![Benchmarks](https://img.shields.io/badge/benchmarks-10-blue)](#raw-throughput)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Skills.sh](https://img.shields.io/badge/skills.sh-kb-purple)](https://skills.sh)

# kb

A 6MB Go binary that compiles codebases into searchable wikis. No embeddings, no vectors, no database.

The LLM reads your code once, turns it into structured articles, and you never pay for that context again. Searches take about 10ms.

## Why

AI agents keep re-reading the same source files. Every conversation, every task, thousands of tokens spent on files that haven't changed. RAG pipelines bolt on vector databases and embedding models to deal with this, but you end up maintaining more infrastructure than the problem warrants.

`kb` does something simpler: compile your codebase into a wiki once, then search it.

```
129 source files  →  kb build  →  129 wiki articles + concept graph
                                       ↓
                                  10ms search, zero re-reads, works offline
```

## Comparison

| | kb | Traditional RAG | Graphify |
|---|---|---|---|
| Approach | LLM compiles at write time | Embedding similarity at query time | Knowledge graph from AST + LLM |
| Search | BM25 + concept boost (~10ms) | Vector similarity + reranking (~200ms) | Graph traversal, BFS/DFS |
| Install | 6MB binary | Python + embedding model + vector DB | Python + NetworkX + tree-sitter + vis.js |
| Languages | Go, Python, TypeScript/JS | Language-agnostic | 14 languages via tree-sitter |
| Output | Markdown wiki articles | Opaque vectors | Interactive graph (HTML/Obsidian/Neo4j) |
| Incremental | SHA256 cache, 0.03s rebuild | Re-embed on change | SHA256 cache, per-file |
| Offline | Yes, after first build | Needs embedding model | Needs Claude for docs/images |
| Search accuracy | 100% on 22 test queries | Depends on embeddings | N/A (different query model) |

kb is fast and simple. One binary, no infra, plain markdown output you can `cat` or `grep` or commit to git. Search returns the right article first on every query we've tested.

Graphify does things kb doesn't: it models relationships between concepts (not just which articles mention them), has a visual graph explorer, handles 14 languages through tree-sitter, and ingests images and PDFs natively. RAG is better when you need fuzzy semantic matching over large unstructured text.

kb handles multimodal by piping external tools into `kb ingest` (see [multimodal](#multimodal)). Graphify builds extraction in natively. Depends on whether you want a lean binary or a batteries-included toolkit.

## Numbers

Tested against real codebases with `claude-haiku-4-5-20251001`. These are not synthetic benchmarks.

### Build speed

| Codebase | Language | Files | Cold build | Per file | Warm build |
|----------|----------|-------|-----------|---------|-----------|
| [litestream](https://github.com/benbjohnson/litestream) | Go | 129 | 355s | 2.75s | 0.03s |
| [flask](https://github.com/pallets/flask) | Python | 83 | 78s | 3.25s | 0.01s |
| [Small corpus](examples/small/) | Go/Py/TS | 10 | 9.5s | 1.90s | 0.01s |

Cold builds run 5 files at a time. Warm builds check SHA256 hashes and skip anything that hasn't changed, which is why they take fractions of a second.

### Search accuracy

| Corpus | Queries | Correct first result | Latency |
|--------|---------|---------------------|---------|
| [Go examples](examples/small/go/) | 6 | 6/6 | 9.8ms |
| [Python examples](examples/small/python/) | 4 | 4/4 | 10.0ms |
| [TypeScript examples](examples/small/typescript/) | 2 | 2/2 | 10.0ms |
| litestream (129 articles) | 5 | 5/5 | ~11ms |
| flask (24 articles) | 5 | 5/5 | ~11ms |

22 for 22. BM25 with title weighting (3x) and concept weighting (2x). No embeddings involved.

Test queries are in [`examples/golden/search_relevance.json`](examples/golden/search_relevance.json).

### Raw throughput

These run offline on an Apple M2 Pro. No API key needed.

| Operation | Speed |
|-----------|-------|
| Go AST parse | 17,586 files/sec |
| Python parse | 4,783 files/sec |
| TypeScript parse | 6,136 files/sec |
| BM25 search (1K articles) | 11ms |
| BM25 search (5K articles) | 55ms |
| Index rebuild (1K articles) | 0.8ms |
| Content hash (10KB file) | 6 microseconds |

### What comes out

| | litestream (129 files) | flask (83 files) |
|---|---|---|
| Articles | 129 | 24 |
| Total words | 74,931 | 13,893 |
| Words per article | 580 avg | 578 avg |
| Concepts | 990 | 263 |
| Concepts per article | 7.6 avg | 10.9 avg |
| Categories | 353 | 112 |

Each article has a title, summary, body, concepts, categories, and backlinks to related articles. They're full documents, not chunks.

You can browse pre-built output here: [`examples/output/`](examples/output/)

## Install

```bash
# Download binary (macOS ARM)
curl -L https://github.com/qbtrix/kb-go/releases/download/v0.1.0/kb-darwin-arm64 -o kb && chmod +x kb

# Build from source
go install github.com/qbtrix/kb-go@latest

# Or clone
git clone https://github.com/qbtrix/kb-go && cd kb-go && go build -o kb .
```

Binaries for macOS and Linux (ARM and x86) are on the [releases page](https://github.com/qbtrix/kb-go/releases).

You need an Anthropic API key for building and ingesting:
```bash
export ANTHROPIC_API_KEY="sk-..."
```

## Quick start

```bash
# Build a wiki
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Search it
kb search "auth middleware" --scope myapp

# Rebuild (only changed files get recompiled)
kb build ./src --scope myapp --pattern "*.go"

# Export as markdown
kb build ./src --scope myapp --output docs/wiki/

# Watch for changes
kb watch ./src --scope myapp --pattern "*.go"

# Pipe in text from other tools
pdftotext paper.pdf - | kb ingest --scope myapp --source "paper.pdf"
```

## Agent integrations

After building a wiki, tell your agent to check it before grepping raw files.

### Claude Code

Add to your project's `CLAUDE.md`:

```markdown
## Knowledge base

Before searching raw files, check the knowledge base:
\`\`\`bash
kb search "<your question>" --scope myapp --context
\`\`\`
Returns pre-compiled articles instead of raw source.
```

Or install as a skill and Claude picks it up automatically:
```bash
npx skills add qbtrix/kb-go
```

### Codex, OpenCode, Cursor, other agents

Add to `AGENTS.md` or your agent's instructions file:

```markdown
## Knowledge base

This project has a pre-built knowledge base. Before searching files:
\`\`\`bash
kb search "<topic>" --scope myapp --context
\`\`\`
Full article: `kb show <article-id> --scope myapp`
Overview: `kb stats --scope myapp`
```

### Programmatic access

`--context` returns formatted text you can inject into prompts:

```bash
CONTEXT=$(kb search "authentication flow" --scope myapp --context)
# Markdown blocks, truncated to ~8K chars
```

`--json` on every command for machine consumption:

```bash
kb search "auth" --scope myapp --json
kb stats --scope myapp --json
kb show auth-service --scope myapp --json
```

## Commands

| Command | Description |
|---------|-------------|
| `kb build <path>` | Scan, parse AST, compile with LLM, build wiki |
| `kb search <query>` | BM25 search over articles |
| `kb ingest [file]` | Ingest a file or piped stdin |
| `kb show <id>` | Print a full article |
| `kb list` | List all articles |
| `kb stats` | Counts for articles, concepts, words |
| `kb lint` | Structural checks; add `--llm` for deeper analysis |
| `kb recompile <id>` | Recompile from raw source (`--all` for everything) |
| `kb watch <path>` | Auto-rebuild on file changes |
| `kb clear` | Wipe all data for a scope |

## Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scope` | `default` | Scope name, for multi-tenant use |
| `--json` | off | JSON output |
| `--model` | `claude-haiku-4-5-20251001` | Model for compilation |
| `--pattern` | `*.py` | File patterns, comma-separated |
| `--concurrency` | `5` | Parallel compilations |
| `--output` | | Export wiki to directory |
| `--lang` | auto | Language for stdin ingest |

## AST parsing

Source files are parsed for structure before the LLM sees them. The compilation prompt includes both the raw code and a structural summary (types, functions, imports), which tends to produce better articles.

| Language | Parser | Extracts |
|----------|--------|----------|
| Go | `go/parser` (stdlib) | packages, structs, interfaces, methods, functions, constants |
| Python | regex | classes, methods, async markers, imports, docstrings, constants |
| TypeScript/JS | regex | classes, interfaces, enums, types, functions, arrow functions |

## How it works

```
Source files
    ↓
AST parse (Go: go/ast, Python: regex, TS: regex)
    ↓
LLM compile (Anthropic API, 5 concurrent)
    ↓
Wiki articles (markdown + JSON frontmatter)
    ↓
BM25 index (pre-tokenized, weighted by title and concepts)
    ↓
~/.knowledge-base/{scope}/
├── raw/       original source, kept for recompilation
├── wiki/      compiled articles as .md files
├── cache/     SHA256 hashes + pre-tokenized search index
└── index.json concept graph, backlinks, categories
```

Articles are plain markdown with JSON frontmatter. You can `cat` them, `grep` across them, or commit them to your repo.

## Multimodal

kb takes text in and puts articles out. If you want to ingest PDFs, images, audio, or web pages, pipe the extraction through whatever tool you prefer:

```bash
# PDFs
pdftotext paper.pdf - | kb ingest --scope research --source "paper.pdf"

# Images / OCR
tesseract diagram.png stdout | kb ingest --scope docs --source "diagram.png"

# Web pages
curl -s https://docs.example.com | kb ingest --scope docs --source "docs-page"

# Audio
whisper meeting.mp3 --output_format txt && cat meeting.txt | kb ingest --scope meetings

# Structured data
cat export.json | jq -r '.messages[].text' | kb ingest --scope comms --source "slack"
```

If you're using Python, the [PocketPaw wrapper](https://github.com/pocketpaw/pocketpaw) handles PDF, URL, OCR, and DOCX extraction and pipes it to kb.

## Architecture

One file: [`kb.go`](kb.go), 2,463 lines. One dependency: `fsnotify` for watch mode.

| Component | ~Lines | What it does |
|-----------|--------|-------------|
| Data models | 100 | RawDoc, WikiArticle, Concept, KnowledgeIndex, Cache |
| AST parsers | 400 | Go (stdlib), Python (regex), TypeScript (regex) |
| Storage | 200 | File CRUD, markdown with JSON frontmatter |
| BM25 search | 100 | Weighted scoring with pre-tokenized index |
| LLM compilation | 100 | Direct HTTP to Anthropic API, no SDK |
| Lint | 150 | Structural checks and LLM analysis |
| CLI | 400 | Commands, flags, JSON output |
| Watch | 80 | fsnotify, 3s debounce |

## Testing

37 unit tests, 10 performance benchmarks. No test dependencies beyond the Go stdlib.

```bash
go test -v ./...       # Unit tests
go test -bench=. ./... # Benchmarks
./bench.sh small       # Full pipeline (needs API key)
```

### Coverage

| Area | Tests | What it checks |
|------|-------|----------------|
| Storage | 6 | Round-trip for articles, raw docs, index; frontmatter parsing |
| Search | 5 | Ranking, empty inputs, limits |
| Cache | 3 | Hits, misses, persistence |
| AST parsers | 6 | Go, Python, TypeScript extraction |
| Lint | 3 | Empty KB, missing concepts, broken backlinks |
| Scanning | 3 | Patterns, skip lists, multi-pattern |
| Helpers | 8 | Slugify, tokenize, hashing, word count |
| Compat | 1 | Python format round-trip |
| Benchmarks | 10 | All core operations at multiple scales |

### Reproduce the benchmarks

```bash
# Offline, runs in seconds
go test -bench=. -benchmem

# Full pipeline on real codebases
./bench.sh small                              # 10 files, ~30s
./examples/fetch.sh all && ./bench.sh medium  # 129 files, ~6 min
./bench.sh all                                # everything

cat bench_results.json
```

## Examples

```
examples/
├── small/              10 source files (Go, Python, TypeScript)
│   ├── go/             server, handler, middleware, models, config
│   ├── python/         service, models, utils
│   └── typescript/     api, types
├── golden/             Expected search results and concept extraction
│   ├── search_relevance.json
│   └── concept_expected.json
├── output/             Pre-built wiki (browse without an API key)
│   ├── small-go/
│   ├── small-python/
│   └── small-typescript/
├── fetch.sh            Download litestream + flask for benchmarking
└── README.md
```

[Source files](examples/small/) · [Expected results](examples/golden/) · [Compiled output](examples/output/)

## License

MIT
