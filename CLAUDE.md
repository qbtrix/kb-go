# kb-go

Headless knowledge base engine. One Go binary (`package main` split into per-concern files), no frameworks.

## Structure

- `main.go` — entry point and command dispatch; `flags.go` — minimal flag parsing
- `cmd_<name>.go` — one file per command (`cmd_build.go`, `cmd_search.go`, `cmd_ingest.go`, `cmd_accept.go`, …) with the helpers only that command uses
- `models.go` — data models; `storage.go` — raw docs, articles, frontmatter, article identity, index, cache
- `search_index.go` — persisted inverted index; `bm25.go` — scoring; `context.go` — `search --context` excerpts
- `compile.go` — compile prompt + the `--compiler` hook (runs the caller's command, parses one JSON article, `usage`); `compiler_shell_{unix,windows}.go` — platform shell + process-tree kill; `lint.go` — structural + LLM lint (via the hook); `export.go`, `watch.go`, `scan.go`, `helpers.go`
- `examples/compilers/` — ready-made compilers: `claude_code.py` (headless Claude Code, user's login), `openai_compatible.py` (LiteLLM proxy / LM Studio / Ollama)
- `astparse.go`, `parse_go.go`, `parse_python.go`, `parse_ts.go` — source parsers that feed compilation
- `mcp.go` — `kb serve` MCP server; `convo.go` — conversation mode; `porter.go` — stemmer; `vsearch.go`, `vector_cli.go` — vectors
- `glossary.go` — Domain glossary support (skip-LLM passthrough + list/show/validate commands)
- `contradiction.go` — Cross-source definition contradiction detection (offline; flags terms two sources define differently)
- `kb_test.go`, `glossary_test.go`, `convo_test.go`, `vsearch_test.go`, `vector_cli_test.go` — unit tests; `compiler_test.go` — hook + no-compiler refusals + usage + version (its TestMain re-execs the test binary as a fake compiler when `KB_FAKE_COMPILER` is set)
- `kb_bench_test.go` — 10 performance benchmarks
- `bench.sh` — Integration benchmark script (full pipeline; build steps need `KB_COMPILER`)
- `SKILL.md` — skills.sh distribution
- `go.mod` — Module: `github.com/qbtrix/kb-go`, one dep: fsnotify

## Commands

```bash
go build -o kb .
go test -v ./...
go test -bench=. -benchmem

# Usage (kb has no LLM client: compile via --compiler "<cmd>" / KB_COMPILER, or prepare/accept)
kb build <path> --scope <name> --pattern "*.go,*.py,*.ts" --compiler "<cmd>"  # exit 2 without a compiler if anything needs compiling; exit 1 if a file failed
kb prepare <path> --scope <name> --pattern "*.go"   # Agent mode: output prompts
kb accept --scope <name>                             # Agent mode: read compiled articles (+ optional usage) from stdin
kb search <query> --scope <name>
kb search <query> --scope <name> --context [--context-chars 4000] [--context-total 8000] [--json]  # prompt-ready excerpts
kb ingest [file] --scope <name> --compiler "<cmd>"   # Fails loudly (exit 1) if the compile fails: raw doc kept, NO article; exit 2 with no compiler
kb ingest [file] --scope <name> --allow-fallback     # Explicit opt-in: store raw text verbatim (no compiler, or when it fails)
kb ingest --article-json --scope <name>              # Read {"raw_text","article"} from stdin — caller supplies the compiled article (+ optional usage)
kb show <id> --scope <name>
kb list --scope <name>
kb stats --scope <name>         # + compile usage totals when articles carry usage
kb lint --scope <name>          # structural (no LLM)
kb lint --scope <name> --llm    # LLM review through --compiler (exit 2 without one)
kb recompile <id> --scope <name>        # needs --compiler
kb recompile --all --scope <name>
kb watch <path> --scope <name>          # needs --compiler
kb version                      # module version from build info, else "dev (<rev>)"
kb glossary list --scope <name>
kb glossary show <term> --scope <name>
kb glossary validate --scope <name>
kb clear --scope <name>
```

## Patterns

- One `package main`, one file per concern; new commands go in their own `cmd_<name>.go`, shared code in the concern file it belongs to
- Manual CLI arg parsing (no cobra/urfave)
- No LLM client: kb never calls a model API and never reads an API key. The caller owns the model so spend stays on its metered path. `--compiler "<cmd>"` (env `KB_COMPILER`, flag wins) runs once per item through the platform shell (`sh -c`; `cmd /S /C` via raw `SysProcAttr.CmdLine` on Windows), prompt on stdin, ONE JSON article on stdout; stderr relayed with a `[compiler <source>]` prefix; `--compiler-timeout` (default 300s) kills the whole process tree. A failed item (exit != 0, timeout, no title/content) gets no article and no cache entry, and the command exits 1 after saving the rest. The removed `--model` flag is rejected with exit 2
- Optional `usage` {model, input_tokens, output_tokens, cost_usd} from the hook, `accept` and `ingest --article-json` is parsed leniently (bad types dropped), stored in frontmatter (`usage`, omitted when absent), replaced on recompile/re-accept, summed by `kb stats`. `compiled_with` = explicit value, else usage.model, else `compiler:<first word>` / `agent` / `external`
- Storage: `~/.knowledge-base/{scope}/` (raw/, wiki/, cache/, index.json)
- Content hash caching (SHA256) for incremental builds
- Parallel compiler runs (5 concurrent goroutines, configurable via --concurrency)
- AST parsing: Go via go/ast (stdlib), Python via regex, TypeScript/JS via regex
- BM25 search with title (3x), concept (2x), and glossary exact-Term/Alias (10x) boosting, scored from a versioned inverted index (`cache/search_index.json`, `{"v":3, postings: term -> [(docIdx, tf)]}`). Tokens are Porter-stemmed (`tokenize()` -> `porterStem`, porter.go) for both index and query, so "open" matches "opens"; the glossary boost stems Term/Alias too. Old-format index files (v1, and pre-stemming v2 whose raw terms would miss stemmed queries) are ignored and search falls back to tokenize-on-the-fly until the next index write (or a full-scope search) upgrades them. Changing tokenization means bumping `searchIndexVersion`
- `search --context`: each hit is `## Title\n` + the whole body if it fits `--context-chars` (default 4000 bytes), else a query-focused excerpt (`contextExcerpt`: sections split at headings, BM25 over the article's own sections, heading terms 3x, chosen sections in document order with `…` for gaps, tables/lists never cut mid-row). Never summary-only. Blocks joined by `\n\n---\n\n` up to `--context-total` (default 8000); the top hit is trimmed, never dropped. Add `--json` for `[{"id","title","text","truncated"}]`, which avoids mis-splitting bodies that contain a `---` rule
- Glossary articles: files under any `glossary/` directory skip compilation and round-trip verbatim (a glossary-only build needs no compiler)
- Loud-fail compile everywhere: a failed compile keeps the raw doc and writes NO article (a silent verbatim fallback poisons search — proven on a 4M-word scope). Only `ingest --allow-fallback` stores verbatim, on explicit opt-in; `--article-json` accepts an externally compiled article on stdin; ingest `--json` output reports `compiled_with`
- MCP article cache (`kb serve`, mcp.go `articleCache`): other processes write scopes while the server runs, so every kb_search/kb_list/kb_stats call re-stats `wiki/` (one ReadDir, mtime+size per file) and `cache/search_index.json`, re-parses only added/changed files and drops vanished ones; a file read within 3s of its mtime is never trusted (same-tick overwrites keep mtime+size). Never key invalidation on index.json or the dir mtime: convo ingest and category normalization write wiki files only, and in-place overwrites leave the dir mtime unchanged
- `--json` flag for machine-readable output on all commands
- Multi-pattern support: `"*.go,*.py,*.ts"` in a single build

## Testing

Unit tests (230+) + 10 benchmarks. No external test deps. On Windows, point both `HOME` and `USERPROFILE` at a temp dir when running the suite (`os.UserHomeDir` reads `USERPROFILE`), or tests write scopes into your real `~/.knowledge-base`.

```bash
go test -v ./...              # All unit tests
go test -bench=. -benchmem    # Performance benchmarks
./bench.sh small              # Integration benchmarks (needs KB_COMPILER)
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
- **Compiler hook:** happy path, shell quoting, non-zero exit / timeout / garbage → loud failure with nothing verbatim, flag-over-env, commands refusing (exit 2) without a compiler, usage storage/replacement/stats totals, no LLM client strings in non-test sources, version fallback

## Relationship to Other Projects

- **c4-gen** shares `~/.knowledge-base/{scope}/` directory. kb-go writes to `wiki/`, c4-gen writes to `c4/`.
- **PocketPaw** can use kb-go via thin Python subprocess wrapper. Heavy extraction (PDF, OCR, URL) stays in Python, pipes text to `kb ingest`.
- **skills.sh** distributes kb-go via SKILL.md.
