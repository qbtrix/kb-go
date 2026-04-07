[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/qbtrix/kb-go?color=blue)](https://github.com/qbtrix/kb-go/releases)
[![Tests](https://img.shields.io/badge/tests-37%20passing-brightgreen)](#testing)
[![Benchmarks](https://img.shields.io/badge/benchmarks-10-blue)](#raw-throughput)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Skills.sh](https://img.shields.io/badge/skills.sh-kb-purple)](https://skills.sh)

# kb — Headless Knowledge Base Engine

A single-binary CLI that compiles codebases into searchable knowledge wikis.

No embeddings. No vectors. No database. No Python runtime. **One 6MB binary.**

Your LLM reads the code **once**, compiles it into structured articles, and never needs to read it again. Searches come back in 10ms.

## Why this exists

AI agents waste tokens re-reading the same files over and over. RAG pipelines try to fix this with vector databases, embedding models, and chunking strategies — but they're approximating understanding, not capturing it.

`kb` flips the approach: **understand once, search forever.**

```
Your codebase (129 files) → kb build → 129 structured articles + concept graph
                                        ↓
                              Search in 10ms, not 10 seconds
                              Zero tokens on re-reads
                              Fully offline after first build
```

## How it stacks up

| | **kb** | **Traditional RAG** | **Graphify** (knowledge graph) |
|---|---|---|---|
| **Approach** | LLM compiles at write time | Embedding similarity at query time | Knowledge graph from AST + LLM |
| **Search** | BM25 + concept boost (~10ms) | Vector similarity + reranking (~200ms) | Graph traversal + BFS/DFS |
| **Install** | **6MB** binary, zero deps | Python + embedding model + vector DB | Python + NetworkX + tree-sitter + vis.js |
| **Languages** | Go, Python, TypeScript/JS | Language-agnostic (embeds raw text) | 14 languages via tree-sitter |
| **What you get** | Readable wiki articles (markdown) | Opaque vector embeddings | Interactive graph (HTML/Obsidian/Neo4j) |
| **Incremental** | SHA256 cache, 0.03s warm build | Re-embed on change | SHA256 cache, per-file |
| **Offline** | Fully offline after build | Needs embedding model running | Needs Claude for docs/images |
| **Search accuracy** | **100%** (22/22 queries) | Depends on embedding quality | N/A (graph queries, not search) |
| **Token savings** | Compile once, never re-read | N/A (embeds, not reads) | 71.5x claimed on mixed corpus |

**Where kb shines** — speed (10ms search, instant warm builds), simplicity (one binary, no infra), and predictability (the right article surfaces first, every time). Output is plain markdown you can `cat`, `grep`, or commit to git.

**Where others win** — Graphify models relationships *between* concepts (not just concept-to-article), supports visual graph exploration, handles 14 languages, and has built-in image/PDF ingestion. RAG is stronger for vague semantic queries over large unstructured text.

`kb` handles multimodal through piping — see [Bring Your Own Batteries](#multimodal--bring-your-own-batteries). Graphify bakes it in. Different philosophy: lean binary vs batteries-included.

## Performance

All numbers from real codebases, not synthetic benchmarks. Model: `claude-haiku-4-5-20251001`.

### Build speed

| Codebase | Language | Files | Cold Build | Per File | Warm Build |
|----------|----------|-------|-----------|---------|-----------|
| [**litestream**](https://github.com/benbjohnson/litestream) | Go | 129 | 355s | 2.75s | **0.03s** |
| [**flask**](https://github.com/pallets/flask) | Python | 83 | 78s | 3.25s | **0.01s** |
| [Small corpus](examples/small/) | Go/Py/TS | 10 | 9.5s | 1.90s | **0.01s** |

Cold builds compile 5 files in parallel. Warm builds skip everything that hasn't changed — SHA256 content hashing makes this close to instant.

### Search accuracy

| Corpus | Queries | Top Result Correct | Avg Latency |
|--------|---------|-------------------|------------|
| [Go examples](examples/small/go/) | 6 | **100%** (6/6) | 9.8ms |
| [Python examples](examples/small/python/) | 4 | **100%** (4/4) | 10.0ms |
| [TypeScript examples](examples/small/typescript/) | 2 | **100%** (2/2) | 10.0ms |
| litestream (129 articles) | 5 | **100%** (5/5) | ~11ms |
| flask (24 articles) | 5 | **100%** (5/5) | ~11ms |

22 out of 22 queries return the right article first. BM25 scoring with title boosting (3x) and concept boosting (2x) handles this well without any embedding overhead.

Test queries: [`examples/golden/search_relevance.json`](examples/golden/search_relevance.json)

### Raw throughput

Offline numbers on Apple M2 Pro — no API key needed to reproduce:

| Operation | Speed |
|-----------|-------|
| Go AST parse | **17,586 files/sec** |
| Python parse | **4,783 files/sec** |
| TypeScript parse | **6,136 files/sec** |
| BM25 search (1K articles) | **11ms** |
| BM25 search (5K articles) | **55ms** |
| Index rebuild (1K articles) | **0.8ms** |
| Content hash (10KB file) | **6 microseconds** |

### What a built wiki looks like

| Metric | litestream (129 files) | flask (83 files) |
|--------|----------------------|-------------------|
| Articles | 129 | 24 |
| Total words | 74,931 | 13,893 |
| Words per article (avg) | 580 | 578 |
| Concepts extracted | 990 | 263 |
| Concepts per article (avg) | 7.6 | 10.9 |
| Categories | 353 | 112 |

These aren't code chunks — each article is a self-contained document with a title, summary, structured content, linked concepts, and backlinks to related articles.

Browse pre-built examples: [`examples/output/`](examples/output/)

## Install

```bash
# Grab the binary (macOS ARM)
curl -L https://github.com/qbtrix/kb-go/releases/download/v0.1.0/kb-darwin-arm64 -o kb && chmod +x kb

# Or build from source
go install github.com/qbtrix/kb-go@latest

# Or clone and build
git clone https://github.com/qbtrix/kb-go && cd kb-go && go build -o kb .
```

Binaries for macOS (ARM/x86) and Linux (ARM/x86) on the [Releases page](https://github.com/qbtrix/kb-go/releases).

You'll need an Anthropic API key for building and ingesting:
```bash
export ANTHROPIC_API_KEY="sk-..."
```

## Quick start

```bash
# Build a wiki from your codebase
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Search it (~10ms)
kb search "auth middleware" --scope myapp

# Rebuild — only changed files get recompiled
kb build ./src --scope myapp --pattern "*.go"

# Export the wiki as markdown files
kb build ./src --scope myapp --output docs/wiki/

# Auto-rebuild when files change
kb watch ./src --scope myapp --pattern "*.go"

# Feed in text from any source
pdftotext paper.pdf - | kb ingest --scope myapp --source "paper.pdf"
```

## Agent integrations

`kb` works with any AI coding assistant. After building your wiki, point your agent at it so it checks the KB before grepping through raw files.

### Claude Code

Add this to your project's `CLAUDE.md`:

```markdown
## Knowledge Base

Before searching raw files, check the knowledge base:
\`\`\`bash
kb search "<your question>" --scope myapp --context
\`\`\`
This returns pre-compiled articles — faster and cheaper than reading source files.
```

Or install as a skill — Claude will use it automatically:
```bash
npx skills add qbtrix/kb-go
```

### Codex / OpenCode / Cursor / Other agents

Add this to your project's `AGENTS.md` (or the agent's equivalent instructions file):

```markdown
## Knowledge Base

A pre-built knowledge base exists for this project. Before searching files, run:
\`\`\`bash
kb search "<topic>" --scope myapp --context
\`\`\`
For full article details: `kb show <article-id> --scope myapp`
For project overview: `kb stats --scope myapp`
```

### Any agent via `--context` flag

The `--context` flag returns formatted text ready for prompt injection:

```bash
# Get context for an agent prompt
CONTEXT=$(kb search "authentication flow" --scope myapp --context)

# Returns markdown blocks, truncated to ~8K chars
# Perfect for injecting into system prompts or tool responses
```

### Any agent via `--json` flag

Every command supports `--json` for machine consumption:

```bash
kb search "auth" --scope myapp --json     # structured results
kb stats --scope myapp --json             # article/concept counts
kb show auth-service --scope myapp --json # full article as JSON
```

## Commands

| Command | What it does |
|---------|-------------|
| `kb build <path>` | Scan files, parse AST, compile with LLM, build wiki |
| `kb search <query>` | BM25 search over compiled articles |
| `kb ingest [file]` | Ingest a file or piped stdin text |
| `kb show <id>` | Show a full article |
| `kb list` | List all articles |
| `kb stats` | Article/concept/word counts |
| `kb lint` | Structural health check (`--llm` for deep analysis) |
| `kb recompile <id>` | Force recompile from raw source (`--all` for everything) |
| `kb watch <path>` | Auto-rebuild on file changes |
| `kb clear` | Delete all data for a scope |

## Flags

| Flag | Default | What it does |
|------|---------|-------------|
| `--scope` | `default` | Knowledge scope name (supports multi-tenant setups) |
| `--json` | off | Machine-readable JSON output |
| `--model` | `claude-haiku-4-5-20251001` | Which model to compile with |
| `--pattern` | `*.py` | File patterns, comma-separated (`*.go,*.py,*.ts`) |
| `--concurrency` | `5` | How many files to compile in parallel |
| `--output` | — | Export wiki to a directory |
| `--lang` | auto | Language hint for stdin ingest (`go`, `python`, `typescript`) |

## AST parsing

Code files get structure-extracted before the LLM sees them. The LLM receives both the raw source and a structural summary — better articles, fewer wasted tokens.

| Language | Parser | Extracts |
|----------|--------|----------|
| Go | `go/parser` (stdlib) | packages, structs, interfaces, methods, functions, constants |
| Python | regex | classes, methods (async), imports, docstrings, constants |
| TypeScript/JS | regex | classes, interfaces, enums, types, functions, arrow functions |

## How it works

```
Source files
    ↓
AST parse (Go: go/ast, Python: regex, TS: regex)
    ↓
LLM compile (Anthropic API, 5 concurrent, token usage tracked)
    ↓
Wiki articles (markdown + JSON frontmatter)
    ↓
BM25 index (pre-tokenized, title/concept boosted)
    ↓
~/.knowledge-base/{scope}/
├── raw/       (original source, kept for recompilation)
├── wiki/      (compiled articles as .md files)
├── cache/     (SHA256 hashes + pre-tokenized search index)
└── index.json (concept graph, backlinks, categories)
```

Every article is a plain `.md` file with JSON frontmatter. No databases, no binary formats. You can `cat` any article, `grep` across the wiki, or commit it to your repo.

## Multimodal — bring your own batteries

`kb` is headless on purpose — it takes text in, puts articles out. Multimodal just means piping the right tool:

```bash
# PDFs
pdftotext paper.pdf - | kb ingest --scope research --source "paper.pdf"

# Images and diagrams
tesseract diagram.png stdout | kb ingest --scope docs --source "diagram.png"

# Web pages
curl -s https://docs.example.com | kb ingest --scope docs --source "docs-page"

# Audio transcripts
whisper meeting.mp3 --output_format txt && cat meeting.txt | kb ingest --scope meetings

# Slack dumps, CSVs, whatever — if it's text, kb can compile it
cat export.json | jq -r '.messages[].text' | kb ingest --scope comms --source "slack"
```

The binary stays small. Your extraction pipeline stays flexible. Anything that outputs text can feed into `kb`.

If you're using Python, the [PocketPaw wrapper](https://github.com/pocketpaw/pocketpaw) handles PDF, URL, OCR, and DOCX extraction out of the box.

## Architecture

**Single file: [`kb.go`](kb.go) (2,463 lines). One dependency: `fsnotify`.**

| Component | ~Lines | Purpose |
|-----------|--------|---------|
| Data models | 100 | RawDoc, WikiArticle, Concept, KnowledgeIndex, Cache |
| AST parsers | 400 | Go (stdlib), Python (regex), TypeScript (regex) |
| Storage | 200 | File-based CRUD, markdown with JSON frontmatter |
| BM25 search | 100 | Title/concept-boosted scoring with pre-tokenized index |
| LLM compilation | 100 | Direct HTTP to Anthropic API — no SDK |
| Lint | 150 | Structural checks (instant) + LLM-powered analysis (deep) |
| CLI | 400 | Commands, flag parsing, JSON output |
| Watch | 80 | fsnotify with 3s debounce |

## Testing

37 unit tests and 10 performance benchmarks. No external test dependencies.

```bash
go test -v ./...          # Unit tests — all 37 passing
go test -bench=. ./...    # Performance benchmarks
./bench.sh small          # Full pipeline benchmarks (needs API key)
```

### What's covered

| Area | Tests | What it checks |
|------|-------|----------------|
| Storage | 6 | Round-trip persistence for articles, raw docs, index. Frontmatter parsing (valid, empty, corrupt) |
| Search | 5 | BM25 ranking correctness, empty inputs, result limits |
| Cache | 3 | Hit/miss detection, file persistence |
| AST parsers | 6 | Go structs/interfaces/methods, Python classes/async/docstrings, TS interfaces/enums/arrows |
| Lint | 3 | Empty KB, missing concepts, broken backlinks |
| Scanning | 3 | Pattern matching, skip lists (.git, node_modules), multi-pattern |
| Helpers | 8 | Slugify, tokenize, hashing, word count, truncation |
| Compat | 1 | Python knowledge-base format round-trip |
| **Benchmarks** | **10** | Every core operation at multiple scale tiers |

### Reproduce the benchmarks

```bash
# Offline — runs in seconds, no API key
go test -bench=. -benchmem

# Full pipeline on real codebases
./bench.sh small                              # 10 files, ~30s
./examples/fetch.sh all && ./bench.sh medium  # 129 files, ~6 min
./bench.sh all                                # Everything

# Machine-readable results
cat bench_results.json
```

## Examples

```
examples/
├── small/              10 hand-written source files
│   ├── go/             server, handler, middleware, models, config
│   ├── python/         service, models, utils
│   └── typescript/     api, types
├── golden/             Expected search results + concept extraction
│   ├── search_relevance.json
│   └── concept_expected.json
├── output/             Pre-built wiki articles (browse without API key)
│   ├── small-go/
│   ├── small-python/
│   └── small-typescript/
├── fetch.sh            Download litestream + flask for benchmarking
└── README.md
```

Check out the [source files](examples/small/), the [expected results](examples/golden/), or the [compiled wiki output](examples/output/).

## License

MIT
