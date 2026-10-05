# kb-go

Headless knowledge base engine. Single-file Go CLI, no frameworks.

## Structure

- `kb.go` — Core logic (build, search, ingest, show, list, stats, lint, recompile, watch, clear)
- `glossary.go` — Domain glossary support (skip-LLM passthrough + list/show/validate commands)
- `contradiction.go` — Cross-source definition contradiction detection (offline; flags terms two sources define differently)
- `kb_test.go`, `glossary_test.go`, `convo_test.go`, `vsearch_test.go`, `vector_cli_test.go` — unit tests
- `kb_bench_test.go` — 10 performance benchmarks
- `bench.sh` — Integration benchmark script (full pipeline with LLM)
- `SKILL.md` — skills.sh distribution
- `go.mod` — Module: `github.com/qbtrix/kb-go`, one dep: fsnotify

## Commands

```bash
go build -o kb .
go test -v ./...
go test -bench=. -benchmem

# Usage
kb build <path> --scope <name> --pattern "*.go,*.py,*.ts"
kb prepare <path> --scope <name> --pattern "*.go"   # Agent mode: output prompts
kb accept --scope <name>                             # Agent mode: read compiled articles from stdin
kb search <query> --scope <name>
kb search <query> --scope <name> --context [--context-chars 4000] [--context-total 8000] [--json]  # prompt-ready excerpts
kb ingest [file] --scope <name>                      # Fails loudly (exit 1) if LLM compile fails: raw doc kept, NO article
kb ingest [file] --scope <name> --allow-fallback     # Old behavior: store raw text verbatim as the article
kb ingest --article-json --scope <name>              # Read {"raw_text","article"} from stdin — caller supplies the compiled article, no API key
kb show <id> --scope <name>
kb list --scope <name>
kb stats --scope <name>
kb lint --scope <name>          # structural (no LLM)
kb lint --scope <name> --llm    # deep LLM-powered
kb recompile <id> --scope <name>
kb recompile --all --scope <name>
kb watch <path> --scope <name>
kb glossary list --scope <name>
kb glossary show <term> --scope <name>
kb glossary validate --scope <name>
kb clear --scope <name>
```

## Patterns

- Single-file CLI, same style as c4-gen
- Manual CLI arg parsing (no cobra/urfave)
- Direct HTTP to Anthropic API (no SDK)
- Storage: `~/.knowledge-base/{scope}/` (raw/, wiki/, cache/, index.json)
- Content hash caching (SHA256) for incremental builds
- Parallel LLM compilation (5 concurrent goroutines, configurable via --concurrency)
- AST parsing: Go via go/ast (stdlib), Python via regex, TypeScript/JS via regex
- BM25 search with title (3x), concept (2x), and glossary exact-Term/Alias (10x) boosting, scored from a versioned inverted index (`cache/search_index.json`, `{"v":3, postings: term -> [(docIdx, tf)]}`). Tokens are Porter-stemmed (`tokenize()` -> `porterStem`, porter.go) for both index and query, so "open" matches "opens"; the glossary boost stems Term/Alias too. Old-format index files (v1, and pre-stemming v2 whose raw terms would miss stemmed queries) are ignored and search falls back to tokenize-on-the-fly until the next index write (or a full-scope search) upgrades them. Changing tokenization means bumping `searchIndexVersion`
- `search --context`: each hit is `## Title\n` + the whole body if it fits `--context-chars` (default 4000 bytes), else a query-focused excerpt (`contextExcerpt`: sections split at headings, BM25 over the article's own sections, heading terms 3x, chosen sections in document order with `…` for gaps, tables/lists never cut mid-row). Never summary-only. Blocks joined by `\n\n---\n\n` up to `--context-total` (default 8000); the top hit is trimmed, never dropped. Add `--json` for `[{"id","title","text","truncated"}]`, which avoids mis-splitting bodies that contain a `---` rule
- Glossary articles: files under any `glossary/` directory skip LLM compilation and round-trip verbatim
- Loud-fail ingest: a failed LLM compile keeps the raw doc, writes NO article, and exits 1 (a silent verbatim fallback poisons search — proven on a 4M-word scope). `--allow-fallback` opts back into verbatim storage; `--article-json` accepts an externally compiled article on stdin (no `ANTHROPIC_API_KEY`); ingest `--json` output reports `compiled_with`
- MCP article cache (`kb serve`, mcp.go `articleCache`): other processes write scopes while the server runs, so every kb_search/kb_list/kb_stats call re-stats `wiki/` (one ReadDir, mtime+size per file) and `cache/search_index.json`, re-parses only added/changed files and drops vanished ones; a file read within 3s of its mtime is never trusted (same-tick overwrites keep mtime+size). Never key invalidation on index.json or the dir mtime: convo ingest and category normalization write wiki files only, and in-place overwrites leave the dir mtime unchanged
- `--json` flag for machine-readable output on all commands
- Multi-pattern support: `"*.go,*.py,*.ts"` in a single build

## Testing

37 unit tests + 10 benchmarks. No external test deps.

```bash
go test -v ./...              # All unit tests
go test -bench=. -benchmem    # Performance benchmarks
./bench.sh small              # Integration benchmarks (needs ANTHROPIC_API_KEY)
```

### Test Categories
- **Storage:** article/rawdoc/index round-trip, frontmatter parsing
- **Search:** BM25 ranking, edge cases (empty query, empty corpus, limits)
- **Cache:** hit/miss detection, round-trip persistence
- **AST:** Go, Python, TypeScript parsers, format output, language detection
- **Lint:** empty KB, missing concepts, broken backlinks
- **Scanning:** pattern matching, directory skipping, multi-pattern
- **Compatibility:** Python knowledge-base format interop
- **Glossary:** frontmatter round-trip, build-skip-LLM verbatim, exact-Term/Alias search boost, list/show/validate

## Relationship to Other Projects

- **c4-gen** shares `~/.knowledge-base/{scope}/` directory. kb-go writes to `wiki/`, c4-gen writes to `c4/`.
- **PocketPaw** can use kb-go via thin Python subprocess wrapper. Heavy extraction (PDF, OCR, URL) stays in Python, pipes text to `kb ingest`.
- **skills.sh** distributes kb-go via SKILL.md.
