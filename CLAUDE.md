# kb-go

Headless knowledge base engine. One Go binary, no frameworks: a thin root `package main` plus folder packages under `internal/`.

## Structure

- `main.go` — entry point, command dispatch, usage text, version; `flags.go` — minimal flag parsing; `compiler_flags.go` — compile-path resolution (`--compiler`/`KB_COMPILER`, `--compiler-timeout`, `--model`, `ANTHROPIC_API_KEY`/`ANTHROPIC_BASE_URL`) and the exit-2 refusals
- `cmd_<name>.go` — one file per command (`cmd_build.go`, `cmd_search.go`, `cmd_ingest.go`, `cmd_accept.go`, `cmd_graph.go`, `cmd_convo.go`, `cmd_glossary.go`, `cmd_serve.go`, …) with the helpers only that command uses; `helpers.go` — small CLI helpers
- `internal/` — the library, one folder package per concern, layered (a package imports only lower layers): layer 0 `textutil`, `model`, `vector`; layer 1 `parse`; the rest still lives in the root `package main` while the split lands
  - `internal/textutil/` — pure text helpers (`Slugify`, `ContentHash`, `WordCount`, `Truncate`, `NilToEmpty`), stdlib only
  - `internal/model/` — shared data types (`WikiArticle`, `RawDoc`, `KnowledgeIndex`, `Concept`, `Cache`, `ArticleUsage`, `LintIssue`, …)
  - `internal/parse/` — source structure extraction (`Code`, `Module`, `FormatContext`): Go via go/ast, Python and TypeScript/JS via regex
  - `internal/vector/` — flat cosine vector index (`Index`, `New`, `Load`, `Cosine`) and `LoadFile` for vector JSON files
- `storage.go` — scope paths, raw docs, articles, frontmatter, article identity, index, cache; `storage_vectors.go` — per-scope vector index persistence
- `search_index.go` — persisted inverted index; `bm25.go` — scoring; `context.go` — `search --context` excerpts; `search_vector.go` — vector + hybrid (RRF) search
- `compile.go` — compile prompt, compile-path dispatch, and the `--compiler` hook (runs the caller's command, parses one JSON article, `usage`); `anthropic.go` — built-in Anthropic Messages client (default path; `ANTHROPIC_BASE_URL`, token usage); `compiler_shell_{unix,windows}.go` — platform shell + process-tree kill
- `lint.go` — structural + LLM lint (same compile path as build); `lint_categories.go` — category normalisation; `export.go` — wiki export; `graph.go` — concept graph build + mermaid/dot render; `watch.go`, `scan.go`
- `examples/compilers/` — ready-made compilers: `claude_code.py` (headless Claude Code, user's login), `openai_compatible.py` (LiteLLM proxy / LM Studio / Ollama)
- `mcp.go` — `kb serve` MCP server; `convo.go` — conversation mode; `porter.go` — stemmer
- `glossary.go` — Domain glossary support (skip-LLM passthrough + list/show/validate)
- `contradiction.go` — Cross-source definition contradiction detection (offline; flags terms two sources define differently)
- `kb_test.go`, `glossary_test.go`, `convo_test.go`, `vector_cli_test.go`, … — unit tests; `compiler_test.go` — in-process hook, usage and version tests; `anthropic_test.go` — in-process built-in client tests; `e2e_test.go` — tests that exec the built binary (both compile paths through every command, precedence, exit-2 refusals, MCP-vs-CLI parity)
- `internal/kbtest/` — shared test plumbing: `kbtest.Main` (every package's TestMain: isolates HOME and USERPROFILE for the whole run, clears `ANTHROPIC_API_KEY` / `ANTHROPIC_BASE_URL` / `KB_COMPILER` so the suite never hits a real API, and acts as the fake compiler when `KB_FAKE_COMPILER` is set), `SetHome`/`IsolatedHome`, repo-root fixture paths (`Path`, `RootPath`), `BuildBinary`/`RunKB`, `NewStubAnthropic` (a local fake Messages API)
- `kb_bench_test.go` — 10 performance benchmarks
- `bench.sh` — Integration benchmark script (full pipeline; build steps need `ANTHROPIC_API_KEY` or `KB_COMPILER`)
- `SKILL.md` — skills.sh distribution
- `go.mod` — Module: `github.com/qbtrix/kb-go`, one dep: fsnotify

## Commands

```bash
go build -o kb .
go test -v ./...
go test -bench=. -benchmem

# Usage. Compile path: --compiler > KB_COMPILER > built-in Anthropic client (ANTHROPIC_API_KEY) > exit 2
kb build <path> --scope <name> --pattern "*.go,*.py,*.ts"                     # built-in client; --model MODEL; ANTHROPIC_BASE_URL for a proxy
kb build <path> --scope <name> --pattern "*.go" --compiler "<cmd>"            # bring your own compiler (--model here is exit 2)
# build: exit 2 with no compile path if anything needs compiling; exit 1 if a file failed
kb prepare <path> --scope <name> --pattern "*.go"   # Agent mode: output prompts
kb accept --scope <name>                             # Agent mode: read compiled articles (+ optional usage) from stdin
kb search <query> --scope <name>
kb search <query> --scope <name> --context [--context-chars 4000] [--context-total 8000] [--json]  # prompt-ready excerpts
kb ingest [file] --scope <name>                      # Fails loudly (exit 1) if the compile fails: raw doc kept, NO article; exit 2 with no compile path
kb ingest [file] --scope <name> --allow-fallback     # Explicit opt-in: store raw text verbatim (no compile path, or when it fails)
kb ingest --article-json --scope <name>              # Read {"raw_text","article"} from stdin — caller supplies the compiled article (+ optional usage)
kb show <id> --scope <name>
kb list --scope <name>
kb stats --scope <name>         # + compile usage totals when articles carry usage
kb lint --scope <name>          # structural (no LLM)
kb lint --scope <name> --llm    # LLM review via the compile path (exit 2 without one)
kb recompile <id> --scope <name>
kb recompile --all --scope <name>
kb watch <path> --scope <name>
kb version                      # module version from build info, else "dev (<rev>)"
kb glossary list --scope <name>
kb glossary show <term> --scope <name>
kb glossary validate --scope <name>
kb clear --scope <name>
```

## Patterns

- One `package main`, one file per concern; new commands go in their own `cmd_<name>.go`, shared code in the concern file it belongs to
- Manual CLI arg parsing (no cobra/urfave)
- Built-in compile is the default: direct HTTP to the Anthropic Messages API (no SDK) when `ANTHROPIC_API_KEY` is set; `--model` (default claude-haiku-4-5-20251001); `ANTHROPIC_BASE_URL` (SDK convention, default https://api.anthropic.com) → `<base>/v1/messages`, so a LiteLLM proxy can meter it. Usage (model + input/output tokens from the response, no cost: no price table) is stored on the article; `compiled_with` = the requested model
- Extension: bring your own compiler. Precedence `--compiler` > `KB_COMPILER` > built-in (key set) > exit 2 listing all options. `--model` with a compiler is exit 2 (the compiler picks its model). `--compiler "<cmd>"` runs once per item through the platform shell (`sh -c`; `cmd /S /C` via raw `SysProcAttr.CmdLine` on Windows), prompt on stdin, ONE JSON article on stdout; stderr relayed with a `[compiler <source>]` prefix; `--compiler-timeout` (default 300s) kills the whole process tree. A failed item on either path (hook: exit != 0, timeout, no title/content; built-in: network error, non-200, unparseable reply) gets no article and no cache entry, and the command exits 1 after saving the rest
- Optional `usage` {model, input_tokens, output_tokens, cost_usd} from the hook, `accept` and `ingest --article-json` is parsed leniently (bad types dropped), stored in frontmatter (`usage`, omitted when absent), replaced on recompile/re-accept, summed by `kb stats`. `compiled_with` = explicit value, else usage.model, else `compiler:<first word>` / `agent` / `external`
- Storage: `~/.knowledge-base/{scope}/` (raw/, wiki/, cache/, index.json)
- Content hash caching (SHA256) for incremental builds
- Parallel compilation (5 concurrent goroutines, configurable via --concurrency)
- AST parsing: Go via go/ast (stdlib), Python via regex, TypeScript/JS via regex
- BM25 search with title (3x), concept (2x), and glossary exact-Term/Alias (10x) boosting, scored from a versioned inverted index (`cache/search_index.json`, `{"v":3, postings: term -> [(docIdx, tf)]}`). Tokens are Porter-stemmed (`tokenize()` -> `porterStem`, porter.go) for both index and query, so "open" matches "opens"; the glossary boost stems Term/Alias too. Old-format index files (v1, and pre-stemming v2 whose raw terms would miss stemmed queries) are ignored and search falls back to tokenize-on-the-fly until the next index write (or a full-scope search) upgrades them. Changing tokenization means bumping `searchIndexVersion`
- `search --context`: each hit is `## Title\n` + the whole body if it fits `--context-chars` (default 4000 bytes), else a query-focused excerpt (`contextExcerpt`: sections split at headings, BM25 over the article's own sections, heading terms 3x, chosen sections in document order with `…` for gaps, tables/lists never cut mid-row). Never summary-only. Blocks joined by `\n\n---\n\n` up to `--context-total` (default 8000); the top hit is trimmed, never dropped. Add `--json` for `[{"id","title","text","truncated"}]`, which avoids mis-splitting bodies that contain a `---` rule
- Glossary articles: files under any `glossary/` directory skip compilation and round-trip verbatim (a glossary-only build needs no key or compiler)
- Loud-fail compile everywhere: a failed compile keeps the raw doc and writes NO article (a silent verbatim fallback poisons search — proven on a 4M-word scope). Only `ingest --allow-fallback` stores verbatim, on explicit opt-in; `--article-json` accepts an externally compiled article on stdin; ingest `--json` output reports `compiled_with`
- MCP article cache (`kb serve`, mcp.go `articleCache`): other processes write scopes while the server runs, so every kb_search/kb_list/kb_stats call re-stats `wiki/` (one ReadDir, mtime+size per file) and `cache/search_index.json`, re-parses only added/changed files and drops vanished ones; a file read within 3s of its mtime is never trusted (same-tick overwrites keep mtime+size). Never key invalidation on index.json or the dir mtime: convo ingest and category normalization write wiki files only, and in-place overwrites leave the dir mtime unchanged
- `--json` flag for machine-readable output on all commands
- Multi-pattern support: `"*.go,*.py,*.ts"` in a single build

## Testing

Unit tests (230+) + 10 benchmarks. No external test deps. Every test package's `TestMain` is `kbtest.Main`, which points both `HOME` and `USERPROFILE` (`os.UserHomeDir` reads `USERPROFILE` on Windows) at a throwaway dir, so the suite never touches your real `~/.knowledge-base`; a test that needs its own home calls `kbtest.SetHome`/`kbtest.IsolatedHome` (never `t.Setenv("HOME", …)` alone). Fixture files resolve from the module root via `kbtest.Path`.

```bash
go test -v ./...              # All unit tests
go test -bench=. -benchmem    # Performance benchmarks
./bench.sh small              # Integration benchmarks (needs ANTHROPIC_API_KEY or KB_COMPILER)
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
- **Compiler hook:** happy path, shell quoting, non-zero exit / timeout / garbage → loud failure with nothing verbatim, flag-over-env, commands refusing (exit 2) with no compile path, `--model` with a compiler → exit 2, usage storage/replacement/stats totals, version fallback
- **Built-in client:** httptest Messages stub via `ANTHROPIC_BASE_URL` — request path/headers/model, usage parsed into the article and `kb stats`, non-200 → loud failure (build exit 1, ingest loud-fail + `--allow-fallback`), `--model`, lint, and precedence (`--compiler` > `KB_COMPILER` > key)

## Relationship to Other Projects

- **c4-gen** shares `~/.knowledge-base/{scope}/` directory. kb-go writes to `wiki/`, c4-gen writes to `c4/`.
- **PocketPaw** can use kb-go via thin Python subprocess wrapper. Heavy extraction (PDF, OCR, URL) stays in Python, pipes text to `kb ingest`.
- **skills.sh** distributes kb-go via SKILL.md.
