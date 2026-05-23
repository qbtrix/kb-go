# kb-go

Headless knowledge base engine. Single-file Go CLI, no frameworks.

## Structure

- `kb.go` — Core logic (build, search, ingest, show, list, stats, lint, recompile, watch, clear)
- `glossary.go` — Domain glossary support (skip-LLM passthrough + list/show/validate commands)
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
kb ingest [file] --scope <name>
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
- BM25 search with title (3x), concept (2x), and glossary exact-Term/Alias (10x) boosting
- Glossary articles: files under any `glossary/` directory skip LLM compilation and round-trip verbatim
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
