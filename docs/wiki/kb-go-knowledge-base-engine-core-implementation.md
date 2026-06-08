---
{
  "title": "kb-go: Knowledge Base Engine — Core Implementation",
  "summary": "The central implementation file of kb-go, a CLI tool that ingests source files, compiles them into structured wiki articles via LLM, and provides BM25 search, concept graphing, linting, caching, and watch-mode incremental rebuilds. It is the single binary that orchestrates the entire knowledge base pipeline.",
  "concepts": [
    "BM25 search",
    "LLM compilation",
    "knowledge base",
    "concept graph",
    "frontmatter",
    "caching",
    "SHA-256",
    "AST parsing",
    "watch mode",
    "incremental build",
    "agent mode",
    "prepare/accept",
    "tokenize",
    "fsnotify",
    "changedFilesSinceRef"
  ],
  "categories": [
    "knowledge base",
    "CLI tool",
    "search",
    "code analysis"
  ],
  "source_docs": [
    "739b21dd18bd3136"
  ],
  "backlinks": null,
  "word_count": 608,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## What kb-go Does

kb-go turns a codebase into a searchable wiki. The workflow is: ingest source files → parse their structure (Go AST, Python regex, TypeScript regex) → build a prompt → send to an LLM → accept the compiled article → index it for BM25 search and concept graphing. The binary also supports agent-mode operation via `prepare`/`accept`, where another process handles LLM calls and pipes results back.

## Commands

| Command | Purpose |
|---|---|
| `build` | Ingest + compile in one step |
| `prepare` | Emit compilation prompts as JSON (agent mode) |
| `accept` | Read compiled articles from stdin and persist |
| `search` | BM25 full-text search across all articles |
| `graph` | Export concept co-occurrence graph (Mermaid or DOT) |
| `ingest` | Store raw source without compiling |
| `show` / `list` | Inspect articles |
| `stats` | Word counts, article counts, token usage |
| `lint` | Structural and LLM-assisted quality checks |
| `recompile` | Force recompile selected articles |
| `watch` | Incremental rebuild on file changes (fsnotify) |
| `clear` | Delete articles or raw docs |

## Storage Layout

Articles are stored as markdown files with a JSON frontmatter block at the top. This dual format is intentional: the markdown body is human-readable and git-diffable, while the JSON frontmatter carries typed metadata (`Concepts`, `Categories`, `Backlinks`, `CompiledWith`, `TargetWords`, etc.) that tools can parse without a full markdown parse. The format is deliberately compatible with the Python `knowledge-base` package.

## Source Parsing

`parseCode` dispatches to `parseGo` (using `go/ast` from the standard library), `parsePython` (regex-based line scanning), or `parseTypeScript` (regex). The Go parser is the most accurate because it uses a real AST; Python and TypeScript parsers are heuristic and may miss nested definitions. `formatCodeContext` converts the parsed `CodeModule` into a structured text block injected into the LLM prompt, giving the model package name, imports, exported types, functions, and doc comments without sending the entire raw source.

## Caching

Every source file is hashed with SHA-256 (`contentHash`). The cache maps `hash → articleID + compiled timestamp`. On a `build` run, files whose hash matches the cache are skipped entirely — the LLM is never called. This keeps incremental builds cheap: only changed files cost API tokens.

## BM25 Search

Articles are tokenized (lowercase, punctuation stripped, stop words removed) and scored with BM25. Two search paths exist: `bm25Search` (recomputes TF-IDF on every call) and `bm25SearchWithIndex` (uses a pre-built `SearchIndex` with title and concept token boosts). The pre-built index is the production path; the on-the-fly variant exists for small corpora or tests.

## Concept Graph

`buildConceptGraph` computes concept co-occurrence: two concepts are connected when they appear together in at least one article. Edge weight is the count of shared articles. The graph can be rendered as Mermaid or Graphviz DOT. Subgraph variants (`buildConceptSubgraph`, `buildArticleSubgraph`) focus on a single concept or article's neighborhood — useful for navigation in large knowledge bases.

## Incremental Builds with `--since`

`changedFilesSinceRef` calls `git diff --name-only <ref>` to get files changed since a given commit. The function rejects refs starting with `-` to prevent command injection (a ref like `--exec=malicious` would otherwise be passed directly to git). Non-git directories trigger a warning and fall back to full rebuild.

## Known Gaps

- Python and TypeScript parsers are regex-based and may miss nested classes, decorators, or dynamic exports.
- `lintLLM` requires an API key and is therefore skipped in offline environments.
- The `zvec` build tag for HNSW-scale vector search is mentioned in comments but not yet implemented.
- Watch mode depends on `fsnotify` (the only external dependency); all other functionality is stdlib-only.