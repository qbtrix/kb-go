---
name: kb
description: Build searchable knowledge bases from any source — codebases, docs, markdown, text. LLM-compiled articles with BM25 search. No embeddings, no vectors. Use when the user needs to build, search, ingest, or manage a knowledge base.
compatibility: Requires Go 1.21+ or pre-built kb binary. ANTHROPIC_API_KEY env var for LLM compilation.
metadata:
  author: pocketpaw
  version: 0.1.0
  tags: knowledge-base search bm25 llm documentation
---

# kb — Headless Knowledge Base Engine

A single-binary CLI that turns files into searchable, LLM-compiled knowledge articles. No embeddings, no vectors — the LLM understands at write time, not query time. BM25 search over compiled articles.

## Setup

```bash
# Install from source
go install github.com/pocketpaw/kb-go@latest

# Or build locally
git clone https://github.com/pocketpaw/kb-go && cd kb-go && go build -o kb .

# Set your API key
export ANTHROPIC_API_KEY="sk-..."
```

## Commands

### Build a knowledge base from a codebase

Scans files, compiles each with LLM into a structured article, indexes concepts and backlinks. Uses SHA256 content hashing — unchanged files are skipped on subsequent builds.

```bash
kb build ./src/myproject --scope myproject
kb build ./src/myproject --scope myproject --pattern "*.go"
kb build ./src/ --scope myapp --model claude-haiku-4-5-20251001
```

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

Deep LLM-powered lint finds inconsistencies, knowledge gaps, missing connections, and stale content:

```bash
kb lint --scope myproject --llm
```

### Watch mode (auto-rebuild)

Watches for file changes and rebuilds automatically. Uses content hashing so only changed files are recompiled.

```bash
kb watch ./src/ --scope myproject --pattern "*.py"
```

### Clear

```bash
kb clear --scope myproject
```

## Workflow Examples

### Build a project wiki from scratch

```bash
kb build ./ee/cloud --scope paw-cloud
kb lint --scope paw-cloud
kb search "authentication" --scope paw-cloud
```

### Incremental updates after code changes

```bash
# Only changed files get recompiled (content hash cache)
kb build ./ee/cloud --scope paw-cloud
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
| `--model` | `claude-haiku-4-5-20251001` | LLM model for compilation |

## Agent Mode (no API key needed)

When running inside an AI agent (Claude Code, Cursor, Codex, etc.), you can compile articles using the agent's own LLM instead of making separate API calls. This means no API key, no extra cost — the agent you're already paying for does the compilation.

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
      "categories": ["infrastructure"]
    }
  ]
}
```

Also accepts a bare array or a single article object.

### When to use agent mode vs direct build

- **Agent mode** (`prepare` + `accept`): When running as a skill inside an AI agent. Uses the agent's LLM. No API key needed. Works with any LLM.
- **Direct build**: When running standalone or in CI. Calls Anthropic API directly. Needs `ANTHROPIC_API_KEY`.

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `ANTHROPIC_API_KEY` | For build/ingest/llm-lint | Anthropic API key (not needed for prepare/accept) |
