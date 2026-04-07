# kb — Headless Knowledge Base Engine

Single-binary CLI that turns codebases into searchable, LLM-compiled knowledge wikis. No embeddings, no vectors — the LLM understands at write time, BM25 searches at read time.

## Why

Reading a 200-file codebase costs thousands of tokens every time. `kb` compiles it once into structured articles with concepts, categories, and backlinks. Search returns relevant articles in ~10ms.

## Install

```bash
go install github.com/pocketpaw/kb-go@latest
# or
git clone https://github.com/pocketpaw/kb-go && cd kb-go && go build -o kb .
```

Requires `ANTHROPIC_API_KEY` for building/ingesting (LLM compilation).

## Quick Start

```bash
# Build a wiki from your codebase
kb build ./src --scope myapp --pattern "*.go,*.py,*.ts"

# Search it
kb search "auth middleware" --scope myapp

# Second build is instant (content-hash cache)
kb build ./src --scope myapp --pattern "*.go"  # skips unchanged files
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
| `--pattern` | `*.py` | File patterns, comma-separated (`*.go,*.py,*.ts`) |
| `--concurrency` | `5` | Parallel LLM compilations |
| `--lang` | auto | Language hint for stdin ingest (`go`, `python`, `typescript`) |

## AST Parsing

Code files get AST-parsed before LLM compilation. The LLM sees both raw source and extracted structure — better articles, fewer wasted tokens.

| Language | Parser | What it extracts |
|----------|--------|-----------------|
| Go | `go/parser` (stdlib) | packages, structs, interfaces, methods, functions, constants |
| Python | regex | classes, methods (async), imports, docstrings, constants |
| TypeScript/JS | regex | classes, interfaces, enums, types, functions, arrow functions |

## How It Works

```
Source files → AST parse → LLM compile → Wiki articles → BM25 index
                                              ↓
                                    ~/.knowledge-base/{scope}/
                                    ├── raw/       (original text)
                                    ├── wiki/      (compiled .md)
                                    ├── cache/     (SHA256 hashes)
                                    └── index.json (concepts, backlinks)
```

Articles are readable markdown with JSON frontmatter. No database.

## Benchmarks

Tested on real codebases with `claude-haiku-4-5-20251001`:

| Codebase | Files | Cold Build | Warm Build | Articles | Concepts | Search Relevance |
|----------|-------|-----------|------------|----------|----------|-----------------|
| litestream (Go) | 129 | 355s (2.75s/file) | 0.03s | 129 | 990 | — |
| flask (Python) | 24 | 78s (3.25s/file) | 0.01s | 24 | 263 | — |
| small-go | 5 | 9.5s (1.90s/file) | 0.01s | 5 | 47 | 100% |
| small-python | 3 | 9.5s (3.16s/file) | 0.01s | 3 | 31 | 100% |
| small-typescript | 2 | 9.4s (4.72s/file) | 0.01s | 2 | 29 | 100% |

Offline operation benchmarks (Apple M2 Pro):

| Operation | Throughput |
|-----------|-----------|
| Go AST parse | 17,586 files/sec |
| Python regex parse | 4,783 files/sec |
| TypeScript regex parse | 6,136 files/sec |
| BM25 search (1K articles) | 11ms |
| BM25 search (5K articles) | 55ms |
| Index rebuild (1K articles) | 0.8ms |
| Content hash (10KB) | 6us |

Run benchmarks yourself:

```bash
# Offline (no API key)
go test -bench=. -benchmem

# Full pipeline (needs API key)
./bench.sh small
./examples/fetch.sh all && ./bench.sh all
```

## Tests

37 unit tests, 10 performance benchmarks.

```bash
go test -v ./...          # Unit tests
go test -bench=. ./...    # Benchmarks
```

### Unit Tests (37)

**Helpers:** TestSlugify, TestSlugifyLongTitle, TestTokenize, TestTokenizeEmpty, TestContentHash, TestWordCount, TestTruncate, TestNilToEmpty

**Storage:** TestSaveLoadArticle, TestSaveLoadRawDoc, TestSaveLoadIndex, TestParseArticleWithFrontmatter, TestParseArticleNoFrontmatter, TestParseArticleBadFrontmatter

**Search:** TestBM25Search, TestBM25SearchNoResults, TestBM25SearchEmptyQuery, TestBM25SearchEmptyCorpus, TestBM25SearchLimit

**Cache:** TestCacheRoundTrip, TestCacheHit, TestCacheMiss

**Index:** TestRebuildIndex

**Lint:** TestLintEmptyKB, TestLintMissingConcepts, TestLintBrokenBacklink

**AST Parsers:** TestParseGoBasic, TestParseGoInvalidSyntax, TestParsePythonBasic, TestParseTypeScriptBasic, TestFormatCodeContext, TestDetectLanguage

**File Scanning:** TestScanDir, TestScanDirSkipsNodeModules, TestScanDirMultiPattern

**Compatibility:** TestPythonFormatCompatibility

### Performance Benchmarks (10)

BenchmarkTokenize, BenchmarkContentHash, BenchmarkSlugify, BenchmarkParseGo, BenchmarkParsePython, BenchmarkParseTypeScript, BenchmarkBM25Search, BenchmarkRebuildIndex, BenchmarkScanDir, BenchmarkFormatCodeContext

## Architecture

Single file: `kb.go` (2,286 lines). One external dependency: `fsnotify` (watch mode).

Sections:
- **Data Models** — RawDoc, WikiArticle, Concept, KnowledgeIndex, Cache, LintIssue
- **AST Parsing** — CodeModule, Go/Python/TypeScript parsers, formatCodeContext
- **Storage** — File-based CRUD, markdown + JSON frontmatter
- **BM25 Search** — Title/concept-boosted BM25 with IDF weighting
- **LLM Compilation** — Direct HTTP to Anthropic API, JSON extraction
- **Structural Lint** — Empty content, missing concepts, broken backlinks, orphan concepts
- **LLM Lint** — Inconsistencies, gaps, missing connections via LLM
- **CLI Commands** — build, search, ingest, show, list, stats, lint, recompile, watch, clear

## Examples

```
examples/
├── small/          # 10 hand-written files (Go/Python/TS task API)
├── golden/         # Expected search results + concepts
├── output/         # Pre-built wiki articles (reference)
├── fetch.sh        # Download medium/large test corpora
└── README.md
```

## License

MIT
