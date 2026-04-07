[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/qbtrix/kb-go?color=blue)](https://github.com/qbtrix/kb-go/releases)
[![Tests](https://img.shields.io/badge/tests-37%20passing-brightgreen)](#testing)
[![Benchmarks](https://img.shields.io/badge/benchmarks-10-blue)](#raw-throughput)
[![License](https://img.shields.io/badge/license-MIT-green)](LICENSE)
[![Skills.sh](https://img.shields.io/badge/skills.sh-kb-purple)](https://skills.sh)

# kb

A 6MB Go binary that builds and maintains structured wikis from any source. Code, docs, research papers, meeting notes, web pages. Feed it text, it gives you a searchable, interlinked knowledge base.

No embeddings, no vectors, no database.

## The idea

This is an implementation of [Karpathy's LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) concept. The core insight: instead of retrieving raw document chunks at query time (RAG), have the LLM compile your sources into a persistent wiki that accumulates knowledge over time.

Three layers:
- **Raw sources** -- your files, immutable, never modified
- **The wiki** -- LLM-generated articles with summaries, concepts, categories, and cross-references
- **The index** -- concept graph, backlinks, categories. BM25 search over the compiled articles

Three operations:
- **Ingest** -- add a source, the LLM reads it, writes a structured article, updates the index
- **Search** -- query the wiki, not the raw files. 10ms, not 10 seconds
- **Lint** -- health-check for contradictions, orphan concepts, missing cross-references

The LLM does the tedious work that kills human-maintained wikis: summarizing, cross-referencing, keeping things consistent. You curate sources and ask questions.

## Not just for code

kb started as a codebase wiki builder, and it's good at that. AST parsing for Go, Python, and TypeScript means the LLM gets structural context alongside raw source.

But the input is text and the output is articles. Anything that produces text can feed kb:

```bash
# Codebases
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Research papers
pdftotext paper.pdf - | kb ingest --scope research --source "arxiv-2401.1234"

# Web pages and documentation
curl -s https://docs.example.com/guide | kb ingest --scope docs --source "setup-guide"

# Meeting notes
cat standup-notes.md | kb ingest --scope team --source "standup-04-07"

# Anything with text
cat slack-export.json | jq -r '.messages[].text' | kb ingest --scope comms --source "slack-general"
```

Each new source gets compiled into a wiki article, linked to existing concepts, and made searchable. The wiki grows and gets more useful over time.

## How it compares to RAG

| | kb (LLM wiki) | Traditional RAG |
|---|---|---|
| When understanding happens | At write time (once) | At query time (every time) |
| What you search | Compiled wiki articles | Raw document chunks |
| Search method | BM25 with concept weighting (~10ms) | Vector similarity + reranking (~200ms) |
| What you store | Readable markdown files | Opaque vector embeddings |
| Infrastructure | One 6MB binary | Python + embedding model + vector DB |
| Incremental updates | SHA256 cache, 0.03s warm rebuild | Re-embed changed documents |
| Offline | Yes, after first build | Needs embedding model running |
| Maintenance | LLM handles cross-refs and consistency | You manage chunking and embedding quality |

RAG is better for fuzzy semantic matching over huge unstructured text where you can't compile everything upfront. kb is better when you want accumulated, structured knowledge that improves as you add sources.

## Numbers

Real codebases, `claude-haiku-4-5-20251001`. Not synthetic benchmarks.

### Build speed

| Source | Type | Files | Cold build | Per file | Warm build |
|--------|------|-------|-----------|---------|-----------|
| [litestream](https://github.com/benbjohnson/litestream) | Go codebase | 129 | 355s | 2.75s | 0.03s |
| [flask](https://github.com/pallets/flask) | Python codebase | 83 | 78s | 3.25s | 0.01s |
| [Small corpus](examples/small/) | Go/Py/TS | 10 | 9.5s | 1.90s | 0.01s |

Cold builds run 5 compilations at a time. Warm builds check SHA256 hashes and skip unchanged files.

### Search accuracy

| Corpus | Queries | Correct first result | Latency |
|--------|---------|---------------------|---------|
| [Go examples](examples/small/go/) | 6 | 6/6 | 9.8ms |
| [Python examples](examples/small/python/) | 4 | 4/4 | 10.0ms |
| [TypeScript examples](examples/small/typescript/) | 2 | 2/2 | 10.0ms |
| litestream (129 articles) | 5 | 5/5 | ~11ms |
| flask (24 articles) | 5 | 5/5 | ~11ms |

22 for 22. BM25 with title weighting (3x) and concept weighting (2x).

Test queries: [`examples/golden/search_relevance.json`](examples/golden/search_relevance.json)

### Raw throughput

Offline, Apple M2 Pro. No API key needed to reproduce.

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

Each article has a title, summary, body, concepts, categories, and backlinks. Full documents, not fragments.

Browse pre-built output: [`examples/output/`](examples/output/)

## Install

```bash
# Download binary (macOS ARM)
curl -L https://github.com/qbtrix/kb-go/releases/download/v0.1.0/kb-darwin-arm64 -o kb && chmod +x kb

# Build from source
go install github.com/qbtrix/kb-go@latest

# Clone and build
git clone https://github.com/qbtrix/kb-go && cd kb-go && go build -o kb .
```

Binaries for macOS and Linux (ARM and x86) on the [releases page](https://github.com/qbtrix/kb-go/releases).

You need an Anthropic API key for building and ingesting:
```bash
export ANTHROPIC_API_KEY="sk-..."
```

## Quick start

```bash
# Build a wiki from a codebase
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Search it
kb search "auth middleware" --scope myapp

# Rebuild (only changed files recompile)
kb build ./src --scope myapp --pattern "*.go"

# Export the wiki as markdown
kb build ./src --scope myapp --output docs/wiki/

# Watch for changes
kb watch ./src --scope myapp --pattern "*.go"
```

## Agent integrations

After building a wiki, tell your agent to check it before reading raw files.

### Claude Code

Add to your project's `CLAUDE.md`:

```markdown
## Knowledge base

Before searching raw files, check the knowledge base:
\`\`\`bash
kb search "<your question>" --scope myapp --context
\`\`\`
Returns compiled articles instead of raw source.
```

Or install as a skill:
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

`--context` returns formatted text for prompt injection:

```bash
CONTEXT=$(kb search "authentication flow" --scope myapp --context)
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

Source files are parsed for structure before the LLM sees them. The compilation prompt includes both the raw code and a structural summary, which tends to produce better articles.

| Language | Parser | Extracts |
|----------|--------|----------|
| Go | `go/parser` (stdlib) | packages, structs, interfaces, methods, functions, constants |
| Python | regex | classes, methods, async markers, imports, docstrings, constants |
| TypeScript/JS | regex | classes, interfaces, enums, types, functions, arrow functions |

## How it works

```
Source (any text)
    ↓
AST parse if code (Go: go/ast, Python: regex, TS: regex)
    ↓
LLM compile (Anthropic API, 5 concurrent)
    ↓
Wiki article (markdown + JSON frontmatter)
    ↓
BM25 index (pre-tokenized, weighted by title and concepts)
    ↓
~/.knowledge-base/{scope}/
├── raw/       original source, kept for recompilation
├── wiki/      compiled articles as .md files
├── cache/     SHA256 hashes + pre-tokenized search index
└── index.json concept graph, backlinks, categories
```

Articles are plain markdown with JSON frontmatter. `cat` them, `grep` them, commit them.

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

37 unit tests, 10 performance benchmarks. No test dependencies beyond Go stdlib.

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
go test -bench=. -benchmem                    # Offline, seconds
./bench.sh small                              # 10 files, ~30s
./examples/fetch.sh all && ./bench.sh medium  # 129 files, ~6 min
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
├── output/             Pre-built wiki (browse without an API key)
├── fetch.sh            Download litestream + flask for benchmarking
└── README.md
```

[Source files](examples/small/) · [Expected results](examples/golden/) · [Compiled output](examples/output/)

## Inspired by

[Karpathy's LLM Wiki](https://gist.github.com/karpathy/442a6bf555914893e9891c11519de94f) -- the idea that LLMs should maintain persistent, structured knowledge instead of re-deriving it from raw sources on every query. kb is that concept as a CLI tool.

## License

MIT
