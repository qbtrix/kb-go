[![Go](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Release](https://img.shields.io/github/v/release/qbtrix/kb-go?color=blue)](https://github.com/qbtrix/kb-go/releases)
[![Tests](https://img.shields.io/badge/tests-69%20passing-brightgreen)](#testing)
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

### LongMemEval (external benchmark)

[LongMemEval](https://github.com/xiaowu0162/LongMemEval) is a 500-question benchmark for long-term conversation memory retrieval (ICLR 2025). Each question has ~50 conversation sessions as a haystack and you need to find the one that contains the answer.

We ran kb's BM25 search over raw session text (no wiki compilation, just tokenize and score):

| Method | R@1 | R@5 | R@10 | Time | Dependencies |
|--------|-----|-----|------|------|-------------|
| **kb BM25** | 85.0% | **95.0%** | 96.6% | 0.5s | zero |
| MemPalace (raw mode) | — | 96.6% | — | minutes | ChromaDB + ONNX + sentence-transformers |

95% recall at 5 with nothing but string tokenization and BM25 scoring. The 1.6-point gap to MemPalace is the gap between a full ML pipeline and a single Go binary.

Breakdown by question type:

| Type | R@5 | Notes |
|------|-----|-------|
| knowledge-update | 100% | BM25 handles fact changes well |
| single-session-user | 98.6% | Strong keyword overlap |
| multi-session | 95.5% | Cross-session works via shared entities |
| temporal-reasoning | 95.5% | Date-adjacent keywords help |
| single-session-assistant | 92.9% | Harder — user-only corpus, assistant answers implicit |
| single-session-preference | 76.7% | Semantic gap — "cocktail" vs "gin and tonic" |

Reproduce: `go test -v -run TestLongMemEval_BM25_Small -timeout 120s .` (requires downloading the dataset from [HuggingFace](https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned)).

Full benchmark harness and error analysis in [`benchmarks/longmemeval/`](benchmarks/longmemeval/).

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

For standalone use, you need an Anthropic API key:
```bash
export ANTHROPIC_API_KEY="sk-..."
```

Running inside an AI agent (Claude Code, Cursor, Codex)? Use [agent mode](#agent-mode) instead — no API key needed.

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

## Use with AI agents

kb works in two modes: search an existing wiki, or build the wiki using the agent's own LLM (no API key).

### Claude Code

**Option A: Install as a skill** (recommended)

```bash
npx skills add qbtrix/kb-go
```

Then ask Claude Code to "build a knowledge base from ./src" and it'll follow the skill instructions.

**Option B: Add to CLAUDE.md**

Drop this in your project's `CLAUDE.md`:

```markdown
## Knowledge base

This project has a knowledge base. Use it before reading raw files.

### Search (existing wiki)
\`\`\`bash
kb search "<topic>" --scope myapp --context
\`\`\`

### Build wiki (agent mode, no API key)
To build or update the wiki, use agent mode:
1. Run `kb prepare ./src --scope myapp --pattern "*.go,*.py,*.ts" --json`
2. For each item in the output, process the `prompt` field yourself
3. Collect results and pipe to: `echo '<json>' | kb accept --scope myapp`

The accept input format:
\`\`\`json
{"scope":"myapp","articles":[{"source":"file.go","hash":"from prepare","raw_id":"from prepare","title":"...","summary":"...","content":"...","concepts":["..."],"categories":["..."]}]}
\`\`\`

### Other commands
- `kb show <article-id> --scope myapp` — full article
- `kb stats --scope myapp` — overview
- `kb lint --scope myapp` — health check
```

Claude Code loads `CLAUDE.md` at session start, so it picks up the kb commands automatically.

### OpenAI Codex

Add to `AGENTS.md` in your project root:

```markdown
## Knowledge base

This project uses `kb` for structured knowledge. Binary must be on PATH.

### Search
\`\`\`bash
kb search "<topic>" --scope myapp --context
\`\`\`

### Build (agent mode)
1. `kb prepare ./src --scope myapp --pattern "*.py" --json` — get compilation prompts
2. Process each item's `prompt` field, output JSON with: title, summary, content, concepts, categories
3. Pipe results: `echo '<json>' | kb accept --scope myapp`
```

### Cursor

Add to `.cursorrules`:

```
This project has a knowledge base. Before searching files, run:
  kb search "<topic>" --scope myapp --context
To build/update the wiki without an API key, use agent mode:
  kb prepare ./src --scope myapp --pattern "*.go" --json
  Then compile each prompt and pipe results to: kb accept --scope myapp
```

### Any other agent

If the agent can run shell commands, the same pattern applies. `kb search "topic" --scope myapp --context` returns formatted text you can inject into prompts. `kb prepare` + `kb accept` handles builds. Add `--json` to any command for machine-readable output.

### Agent mode explained

If kb is running inside an agent, the agent already has LLM access. A separate API call means paying twice and managing another key. Agent mode avoids that by splitting the build into two steps:

```bash
# Step 1: Scan files, check cache, output prompts (no LLM call)
kb prepare ./src --scope myapp --pattern "*.go,*.py,*.ts"
# Returns: {"items": [{"source": "main.go", "hash": "...", "raw_id": "...", "prompt": "..."}], ...}

# Step 2: Agent compiles each prompt using its own LLM
# (This is the part where YOUR agent does the work)

# Step 3: Feed compiled results back
echo '<json>' | kb accept --scope myapp
# Returns: {"accepted": 5, "articles": 12, "concepts": 47}
```

`accept` reads JSON from stdin. It accepts a wrapped object `{"scope":"...","articles":[...]}`, a bare array `[{...}]`, or a single article `{...}`.

Each article needs: `source`, `hash`, `raw_id` (from prepare output), plus `title`, `summary`, `content`, `concepts`, `categories` (from your LLM compilation).

Cache works across both modes. Run `prepare` again and unchanged files are skipped.

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

## Concept graph export

Every wiki already has a concept graph built in — articles are connected via shared concepts. `kb graph` exports that graph in portable formats so you can visualize it with whatever tool you want.

```bash
# Overview: top N concepts by article count, edges for co-occurring concepts
kb graph --scope myapp --limit 30

# Focus on one concept (one-hop neighborhood)
kb graph --scope myapp --concept "authentication"

# Focus on one article and its concepts
kb graph --scope myapp --article auth-service

# Other formats
kb graph --scope myapp --format dot | dot -Tpng > graph.png
kb graph --scope myapp --format json > graph.json
```

Mermaid output works directly in GitHub READMEs, Obsidian, Notion, and anything that renders Mermaid. DOT output pipes into Graphviz. JSON is raw node/edge data for custom tooling.

Flags:
- `--format mermaid|dot|json` — output format (default `mermaid`)
- `--concept <name>` — one-hop subgraph around a concept
- `--article <id>` — article and all its concepts
- `--limit N` — cap concepts in overview (default 30)
- `--min-articles N` — only include concepts appearing in ≥N articles (default 2)

## Pairing with Soul Protocol

kb and [Soul Protocol](https://github.com/qbtrix/soul-protocol) solve different halves of the same problem. Wire them together in your agent pipeline and you get something most AI tools don't have: memory of what happened plus a structured view of what exists now.

### The split

| | kb | soul |
|---|---|---|
| What it stores | Articles compiled from sources | Episodic, semantic, procedural memory tied to an agent's identity |
| Where content comes from | Files (code, docs, text) | Conversations, decisions, experiences |
| Regenerable? | Yes — re-run `kb build` | No — memory is earned, not recomputed |
| Decays? | Never | Yes — via ACT-R activation and significance gating |
| Search | BM25 over compiled articles | Relevance + recency + importance weighting |
| Portable format | Markdown + JSON frontmatter | `.soul` ZIP file |

kb answers "how does X work?" from the current codebase. soul answers "what did we decide about X, and why?" from accumulated conversations.

### When to use which

- **Ask kb when** the answer is derivable from source files: implementation details, architecture, API shapes, which module does what
- **Ask soul when** the answer requires remembering a conversation: past decisions, user preferences, relationship context, why something was built a certain way
- **Ask both when** you want the full picture: *why was this built (soul) AND how does it work now (kb)*

### The integration pattern

Keep both as standalone tools. Bridge them in your agent pipeline, not in their code. The cleanest place is wherever your agent builds its system prompt — inject soul-recalled memories alongside kb-searched articles in the same context-building step.

Rough shape (pseudocode):

```python
# In your agent's context builder, run both queries in parallel
soul_memories = await soul.recall(query, limit=5)
kb_articles = subprocess.run(["kb", "search", query, "--scope", project, "--context"])

# Merge into the system prompt with clear framing
system_prompt += f"""
## What we've discussed before (from memory)
{format_soul_memories(soul_memories)}

## What the codebase currently says (from knowledge base)
{kb_articles.stdout}
"""
```

In PocketPaw, this belongs in `src/pocketpaw/bootstrap/context_builder.py` as a new injection block alongside the existing `memory_context` and soul provider. One extra step in the pipeline, no new CLI, no tight coupling.

### Workflow patterns

**1. Pipe soul memories into kb**

Make episodic memories searchable alongside code articles. Useful when you want BM25 search to surface past conversations by concept, not just by date.

```bash
# Pull recent important memories from soul, compile into kb as structured articles
soul recall .soul/my-agent.soul --recent 20 --min-importance 7 --format text \
  | kb ingest --scope my-sessions --source "soul-$(date +%Y-%m-%d)"
```

Now `kb search "authentication decisions"` returns both "this is how auth works now" (code article) and "here's when we decided JWT and why" (compiled soul memory).

**2. Soul references kb articles**

Instead of storing long facts in soul, store short memories with kb pointers. Soul stays lean, the agent still has access to the rich structured knowledge.

```bash
# Soul stores the decision, kb has the details
soul remember .soul/my-agent.soul "Decided JWT auth. Details: kb://paw-cloud/auth-core" --importance 8
```

On recall, your agent sees the memory, follows the `kb://` pointer, and runs `kb show auth-core --scope paw-cloud` to get the current state.

**3. Unified context injection**

Both queries run on every agent request. The agent gets what was decided (soul) plus what the code currently looks like (kb). This is the pattern that matters most — it's the difference between an agent that feels like a coworker versus one that has to be re-briefed every session.

### Why not merge them into one tool?

Different failure modes. kb needs to rebuild from source whenever code changes — it's a cache of your files. Soul can't be rebuilt — losing a `.soul` file means losing experiences that only happened once. Keeping them separate means each one can be backed up, versioned, shared, or deleted independently. The 6MB binary stays a 6MB binary. The `.soul` file stays portable across platforms.

The bridge is thin by design. Any agent pipeline can wire them together in ~20 lines.

## Commands

| Command | Description |
|---------|-------------|
| `kb build <path>` | Scan, parse AST, compile with LLM, build wiki |
| `kb prepare <path>` | Output compilation prompts as JSON (agent mode) |
| `kb accept` | Read compiled articles from stdin (agent mode) |
| `kb graph` | Export concept graph (mermaid, dot, or json) |
| `kb search <query>` | BM25 search over articles |
| `kb ingest [file]` | Ingest a file or piped stdin |
| `kb show <id>` | Print a full article |
| `kb list` | List all articles |
| `kb stats` | Counts for articles, concepts, words |
| `kb lint` | Structural checks; add `--llm` for deeper analysis |
| `kb recompile <id>` | Recompile from raw source (`--all` for everything) |
| `kb watch <path>` | Auto-rebuild on file changes |
| `kb convo ingest <file>` | Parse a conversation transcript, extract entities/decisions/topics, create wiki articles |
| `kb convo search <query>` | Search conversation articles |
| `kb convo list` | List conversation articles |
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

Three files: [`kb.go`](kb.go) (core, ~2,900 lines), [`convo.go`](convo.go) (conversation mode, ~530 lines), [`vsearch.go`](vsearch.go) (vector search primitives, ~140 lines). One dependency: `fsnotify` for watch mode.

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

69 unit tests, 13 performance benchmarks. No test dependencies beyond Go stdlib.

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
