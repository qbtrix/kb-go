---
{
  "title": "kb-go Unit Test Suite",
  "summary": "Comprehensive unit tests for the kb-go knowledge base engine, covering storage round-trips, BM25 search edge cases, caching, linting, directory scanning, AST parsing for three languages, concept graph generation, and the `--since` incremental build flag. Acts as both a regression guard and living documentation of expected behavior.",
  "concepts": [
    "BM25 search",
    "frontmatter",
    "cache",
    "lint",
    "scanDir",
    "AST parsing",
    "backward compatibility",
    "command injection",
    "incremental build",
    "git diff",
    "tempScope",
    "round-trip testing",
    "node_modules exclusion"
  ],
  "categories": [
    "testing",
    "unit tests",
    "knowledge base",
    "Go",
    "test"
  ],
  "source_docs": [
    "dc1e5ebb0fb7525c"
  ],
  "backlinks": null,
  "word_count": 570,
  "compiled_at": "2026-04-23T17:59:23Z",
  "compiled_with": "agent",
  "version": 1,
  "audience": "human",
  "depth": "deep",
  "target_words": 500
}
---

## Scope

`kb_test.go` is the primary quality gate for kb-go. It tests the storage layer, search engine, cache, linter, file scanner, language parsers, graph builder, and the incremental `--since` build feature. Tests are purely offline — no LLM calls, no network access.

## Storage Tests

`TestSaveLoadArticle` and `TestSaveLoadRawDoc` verify the full persistence round-trip: write a struct, read it back, compare field by field. `TestParseArticleWithFrontmatter`, `TestParseArticleNoFrontmatter`, and `TestParseArticleBadFrontmatter` cover the three cases the markdown+JSON frontmatter parser must handle — well-formed, absent, and malformed frontmatter. Malformed frontmatter must not panic; the parser falls back gracefully.

`TestFrontmatterAudienceDepthRoundTrip` specifically guards the `Audience`, `Depth`, and `TargetWords` fields added later in the project's life. These fields must survive a save/load cycle without being silently dropped, which would corrupt the compilation metadata.

`TestFrontmatterLoadBackwardCompat` verifies that old `.md` files written before those fields existed still load without error — a forward-compatibility guarantee for existing knowledge bases.

## BM25 Search Tests

Five tests cover BM25 search behavior:

- `TestBM25Search` — basic relevance ranking
- `TestBM25SearchNoResults` — query with no matching terms returns an empty slice, not nil or an error
- `TestBM25SearchEmptyQuery` — empty string query is handled safely
- `TestBM25SearchEmptyCorpus` — search over zero articles returns empty results
- `TestBM25SearchLimit` — the `limit` parameter caps results correctly

These edge cases matter because callers often pass user-provided queries directly; a panic on empty input or empty corpus would be a production bug.

## Cache Tests

`TestCacheRoundTrip`, `TestCacheHit`, and `TestCacheMiss` verify the SHA-256-based cache. A cache hit must return the correct article ID without recompiling. A cache miss must not return a stale ID. This directly protects the cost model: a broken cache hit would silently recompile every article on every run.

## Lint Tests

`TestLintEmptyKB`, `TestLintMissingConcepts`, and `TestLintBrokenBacklink` validate the structural linter. A broken backlink — an article referencing a concept that no longer exists — must be flagged as a lint issue, not silently ignored.

## Directory Scanner Tests

`TestScanDirSkipsNodeModules` confirms that `node_modules/` directories are excluded during file discovery. Without this, a JavaScript project with a knowledge base would ingest thousands of vendor files, overwhelming the LLM budget.

## Parser Tests

`TestParseGoBasic`, `TestParseGoInvalidSyntax`, `TestParsePythonBasic`, and `TestParseTypeScriptBasic` cover language detection and structure extraction. `TestParseGoInvalidSyntax` is particularly important: the Go AST parser must return a graceful error, not panic, when given syntactically invalid input — which can happen with partially written files in watch mode.

## Incremental Build (`--since`) Tests

Five tests validate the git-based incremental build:

- `TestChangedFilesSinceRef` — only changed files are returned
- `TestBuildSinceRefSkipsUnchanged` — unchanged files are not compiled
- `TestBuildSinceNonGitFallback` — non-git directories warn and fall back to full rebuild
- `TestChangedFilesSinceRefRejectsOptionLikeRef` — refs starting with `-` are rejected to prevent command injection
- `TestChangedFilesSinceRefNonexistentRef` — nonexistent refs produce an error, not a silent empty result

The injection-prevention test is a security fixture: `git diff --name-only <ref>` passes the ref directly to the shell, so a ref value like `--exec=malicious` would execute arbitrary code without this guard.

## Test Helpers

`tempScope` creates a temporary directory scoped to a single test and registers cleanup via `t.Cleanup`. `initGitRepo` and `gitCommitAll` set up real git repositories for the `--since` tests, avoiding mocks that could mask real git behavior.

## Known Gaps

- `TestCompilePromptTerseModeShorterTarget` is listed but its doc is truncated in the AST extraction; the exact terse-mode word-count contract is not fully documented in comments.
- LLM-assisted lint (`lintLLM`) has no unit test — it requires API access.