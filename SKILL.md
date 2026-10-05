---
name: kb
description: Build searchable knowledge bases from any source — codebases, docs, markdown, text. LLM-compiled articles with BM25 search; no embedding model or vector database inside. Use when the user needs to build, search, ingest, or manage a knowledge base.
compatibility: Requires Go 1.25+ or a pre-built kb binary. ANTHROPIC_API_KEY for the built-in compiler, or bring your own (agent mode prepare/accept, or a --compiler command / KB_COMPILER).
metadata:
  author: qbtrix
  version: 0.4.0
  tags: knowledge-base search bm25 llm documentation
---

# kb — Headless Knowledge Base Engine

A single-binary CLI that turns files into searchable, LLM-compiled knowledge articles. No embedding model or vector database inside — the LLM understands at write time, not query time. BM25 search over compiled articles.

## Setup

```bash
# Install from source
go install github.com/qbtrix/kb-go@latest

# Or build locally
git clone https://github.com/qbtrix/kb-go && cd kb-go && go build -o kb .

# Standalone builds: the built-in Anthropic client
export ANTHROPIC_API_KEY="sk-..."
# Optional: route it through a LiteLLM (or other) proxy
# export ANTHROPIC_BASE_URL="http://localhost:4000"

# Or bring your own compiler (kb pipes each prompt to it and reads one JSON
# article back); it wins over the built-in client
# export KB_COMPILER="python /path/to/kb-go/examples/compilers/claude_code.py"
```

Builds compile through, in order: `--compiler "<cmd>"`, `KB_COMPILER`, then the
built-in client when `ANTHROPIC_API_KEY` is set. With none, they exit 2. Inside
an agent, use Agent Mode (`kb prepare` → you compile → `kb accept`, below): no
key needed. Search, show, list, stats, structural lint and glossary commands
need no LLM at all.

## Commands

### Build a knowledge base from a codebase

Scans files, compiles each with the LLM into a structured article, indexes concepts and backlinks. Uses SHA256 content hashing — unchanged files are skipped on subsequent builds. With no key and no compiler, `kb build` exits 2 when anything needs compiling; inside an agent, use Agent Mode (below) instead.

```bash
kb build ./src/myproject --scope myproject                       # built-in client
kb build ./src/ --scope myapp --model claude-haiku-4-5-20251001  # built-in client, explicit model
kb build ./src/myproject --scope myproject --compiler "python examples/compilers/claude_code.py"
kb build ./src/ --scope myapp --compiler-timeout 120s --concurrency 3
```

A file whose compile fails (non-zero exit, timeout, output that isn't one JSON article) gets no article, is retried on the next build, and makes the command exit 1. The raw text is never stored as the article.

### Search the knowledge base

BM25 keyword search over compiled articles. Returns ranked results.

```bash
kb search "auth middleware" --scope myproject
kb search "database connection" --scope myproject --limit 10
kb search "GroupService" --scope myproject --json
```

For agent prompt injection (formatted context block):

```bash
kb search "auth" --scope myproject --context
```

### Ingest a single file or piped text

```bash
# File
kb ingest ./ARCHITECTURE.md --scope myproject

# Piped text (from URL extraction, PDF parsing, etc.)
echo "extracted text here" | kb ingest --scope myproject --source "https://docs.example.com"
cat README.md | kb ingest --scope myproject --source "readme"
```

Ingest compiles like build (compiler, else the built-in client). With neither it
exits 2; if the compile fails it keeps the raw doc but writes NO article and
exits 1.
Two ways forward:

```bash
# Store the raw text verbatim as an article (explicit opt-in, no LLM)
cat notes.md | kb ingest --scope myproject --allow-fallback

# Supply an article you compiled yourself (optional "usage" is recorded)
echo '{"raw_text": "original text", "article": {"title": "My Doc", "summary": "…", "content": "compiled article body", "concepts": ["a"], "categories": ["b"], "source": "notes.md", "compiled_with": "my-backend"}}' \
  | kb ingest --article-json --scope myproject
```

### Show a full article

```bash
kb show auth-middleware --scope myproject
kb show auth-middleware --scope myproject --json
```

### List all articles

```bash
kb list --scope myproject
kb list --scope myproject --json
```

### Statistics

```bash
kb stats --scope myproject
kb stats --scope myproject --json
```

### Lint (health check)

Structural lint runs instantly with no LLM call — checks for empty content, missing concepts, broken backlinks, orphan concepts, and isolated articles.

```bash
kb lint --scope myproject
```

The LLM review finds inconsistencies, knowledge gaps, missing connections, and stale content. It sends one prompt through the same compile path as build and expects a JSON array of issues back:

```bash
kb lint --scope myproject --llm
```

### Watch mode (auto-rebuild)

Watches for file changes and rebuilds automatically. Uses content hashing so only changed files are recompiled. Needs a key or a compiler (exit 2 without one).

```bash
kb watch ./src/ --scope myproject --pattern "*.py"
```

### Concept graph export

Export the wiki's concept graph as Mermaid, Graphviz DOT, or JSON:

```bash
# Top concepts and their connections (default mermaid)
kb graph --scope myproject --limit 30

# One-hop subgraph around a concept
kb graph --scope myproject --concept "authentication"

# Article's concepts
kb graph --scope myproject --article auth-service

# Render with Graphviz
kb graph --scope myproject --format dot | dot -Tpng > graph.png
```

Mermaid output drops directly into GitHub, Obsidian, or Notion.

### Domain glossary

Hand-curate definitions for project-specific terms LLMs default to misreading (Pocket, Soul, Fabric, etc.). Drop markdown files into any `glossary/` directory and `kb build` indexes them without LLM rewriting — body preserved byte-for-byte.

```bash
# Frontmatter shape: kind/term/aliases/category/related
# (see README "Domain glossary" section for the full schema)

kb glossary list --scope myproject
kb glossary show Pocket --scope myproject       # by canonical term
kb glossary show pkt --scope myproject          # or by alias (case-insensitive)
kb glossary validate --scope myproject          # dup terms, alias collisions, dangling refs
```

`kb search` boosts glossary entries 10× when a query token exactly matches a `Term` or `Alias`, so hand-curated definitions outrank mention-heavy module articles.

### Clear

```bash
kb clear --scope myproject
```

## Workflow Examples

### Build a project wiki from scratch

```bash
kb build ./ee/cloud --scope myapp
kb lint --scope myapp
kb search "authentication" --scope myapp
```

### Incremental updates after code changes

```bash
# Only changed files get recompiled (content hash cache)
kb build ./ee/cloud --scope myapp
```

### Feed extracted content from external sources

Heavy extraction (PDF, URL, OCR) happens outside kb, then text is piped in:

```bash
# Python extraction → kb
python -c "import trafilatura; print(trafilatura.extract(...))" | kb ingest --scope myproject --source "https://..."

# PDF text → kb
pdftotext document.pdf - | kb ingest --scope myproject --source "document.pdf"
```

### Agent context injection

```bash
# Get formatted context for an agent prompt
CONTEXT=$(kb search "relevant topic" --scope myproject --context)
# Inject into agent system prompt
```

## Storage

Articles are stored as readable markdown with JSON frontmatter:

```
~/.knowledge-base/{scope}/
├── raw/           # Original ingested content (JSON)
├── wiki/          # Compiled articles (markdown + JSON frontmatter)
├── cache/         # Content hash cache for incremental builds
└── index.json     # Concept graph, backlinks, categories
```

All files are human-readable. No database required.

## Global Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--scope` | `default` | Knowledge scope name (supports multi-tenant) |
| `--json` | off | Machine-readable JSON output |
| `--model` | `claude-haiku-4-5-20251001` | Model for the built-in client (exit 2 if combined with a compiler) |
| `--compiler` | `$KB_COMPILER` | Bring your own compiler: prompt on stdin, one JSON article on stdout. Runs via `sh -c` (`cmd /S /C` on Windows). Wins over the built-in client |
| `--compiler-timeout` | `300s` | Per-item compiler timeout |

## Agent Mode (no API key or compiler needed)

When running inside an AI agent (Claude Code, Cursor, Codex, etc.), compile articles with the agent's own LLM. No API key, no extra cost — the agent you're already using does the compilation.

### Step 1: Get compilation prompts

```bash
kb prepare ./src --scope myapp --pattern "*.go,*.py,*.ts"
```

Returns JSON with a `items` array. Each item has a `prompt` field containing the compilation prompt, plus `source`, `hash`, and `raw_id` for tracking.

### Step 2: Compile each prompt

Process each item's `prompt` field using your own LLM. The prompt asks for JSON output with: `title`, `summary`, `content`, `concepts`, `categories`.

### Step 3: Feed results back

```bash
echo '<json>' | kb accept --scope myapp
```

Input format (JSON object with articles array):
```json
{
  "scope": "myapp",
  "articles": [
    {
      "source": "main.go",
      "hash": "from prepare output",
      "raw_id": "from prepare output",
      "title": "Main Server Entry Point",
      "summary": "...",
      "content": "...",
      "concepts": ["http", "server"],
      "categories": ["infrastructure"],
      "usage": {"model": "your-model", "input_tokens": 900, "output_tokens": 450, "cost_usd": 0.003}
    }
  ]
}
```

Also accepts a bare array or a single article object. `usage` (and `compiled_with`) are optional; `kb stats` totals usage across articles.

### When to use agent mode, the built-in client, or a compiler command

- **Agent mode** (`prepare` + `accept`): When running as a skill inside an AI agent. Uses the agent's LLM. Works with any LLM.
- **Built-in client** (`ANTHROPIC_API_KEY`): When running standalone or in CI with an Anthropic key. `ANTHROPIC_BASE_URL` sends it through a LiteLLM proxy; token usage lands on each article.
- **Compiler command** (`--compiler` / `KB_COMPILER`): When you want another model or provider. kb runs your command once per file: headless Claude Code (`examples/compilers/claude_code.py`), a LiteLLM proxy or any OpenAI-compatible endpoint (`examples/compilers/openai_compatible.py`), Ollama. Recipes and measured costs: README, "Compiling articles".

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | For build/ingest/recompile/watch/`lint --llm`, unless a compiler is configured | Enables the built-in Anthropic client. Not needed for prepare/accept, `ingest --article-json`, search, or structural lint |
| `ANTHROPIC_BASE_URL` | No | Endpoint root for the built-in client (default `https://api.anthropic.com`; requests go to `<base>/v1/messages`), e.g. a LiteLLM proxy |
| `KB_COMPILER` | No | Bring-your-own compiler command; wins over the built-in client, `--compiler` wins over it |
