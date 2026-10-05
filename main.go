// kb is a headless knowledge-base CLI: it compiles source files and docs into
// LLM-written wiki articles, then answers queries with BM25 search over them.
// This file holds the entry point, command dispatch, usage text and the
// package-wide constants (model, base dir, API endpoint, BM25 parameters).
// Storage is markdown with JSON frontmatter; the only external dep is fsnotify
// (watch mode).
package main

import (
	"fmt"
	"os"
)

const (
	defaultModel   = "claude-haiku-4-5-20251001"
	defaultBaseDir = ".knowledge-base"
	apiURL         = "https://api.anthropic.com/v1/messages"
	apiVersion     = "2023-06-01"
	bm25K1         = 1.2
	bm25B          = 0.75
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "build":
		cmdBuild(args)
	case "prepare":
		cmdPrepare(args)
	case "accept":
		cmdAccept(args)
	case "graph":
		cmdGraph(args)
	case "search":
		cmdSearch(args)
	case "ingest":
		cmdIngest(args)
	case "show":
		cmdShow(args)
	case "list":
		cmdList(args)
	case "stats":
		cmdStats(args)
	case "lint":
		cmdLint(args)
	case "clear":
		cmdClear(args)
	case "delete":
		cmdDelete(args)
	case "recompile":
		cmdRecompile(args)
	case "watch":
		cmdWatch(args)
	case "convo":
		cmdConvo(args)
	case "serve":
		cmdServe(args)
	case "glossary":
		cmdGlossary(args)
	case "version":
		fmt.Println("kb v0.1.0")
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`kb — Headless knowledge base engine

Usage: kb <command> [options]

Commands:
  build <path>           Scan files, compile with LLM, build KB
  prepare <path>         Output compilation prompts as JSON (agent mode, no API key)
  accept                 Read compiled articles from stdin (agent mode companion)
  graph                  Export concept graph (mermaid, dot, or json)
  search <query>         BM25 search over compiled articles
  ingest [file]          Ingest a file or stdin text. Fails loudly if LLM
                         compilation fails (raw doc kept, no article).
                         Flags: --allow-fallback (store raw text verbatim
                         instead), --article-json (read {"raw_text","article"}
                         from stdin — caller supplies the compiled article,
                         no API key needed)
  show <article_id>      Show a full article
  list                   List all articles
  stats                  Show KB statistics
  lint                   Structural health check. Flags: --llm (deep LLM
                         check), --normalize-categories [--apply] (collapse
                         variant labels like "CLI"/"cli" to a canonical form)
  recompile <id|--all>   Force recompile article(s) from raw source
  delete <article_id>    Remove one article from a scope (files, index,
                         concept graph, caches). Idempotent.
  clear                  Delete all knowledge for a scope
  watch <path>           Auto-rebuild on file changes
  convo <sub>            Conversation mode (ingest, search, list)
  serve                  Expose read-only KB tools over MCP on stdio
  glossary <sub>         Domain glossary (list, show, validate)
  version                Show version

Global flags:
  --scope NAME           Knowledge scope (default: "default")
  --json                 Output as JSON (for machine consumption)
  --model MODEL          LLM model for compilation (default: claude-haiku-4-5-20251001)
  --concurrency N        Parallel LLM compilations (default: 5, build only)
  --pattern GLOB         File patterns, comma-separated (e.g. "*.go,*.py,*.ts")
  --lang LANG            Language hint for stdin ingest (go, python, typescript)

Examples:
  kb build ./src/myapp --scope myapp
  kb search "auth middleware" --scope myapp
  kb search "shoe sizes" --scope shop --context --context-chars 2000   # prompt-ready excerpts
  kb ingest ./README.md --scope myapp
  echo "some text" | kb ingest --scope myapp --source "notes"
  kb ingest --vec ./vec.json --id article-1 --scope myapp        # attach embedding
  kb search --query-vec ./qvec.json --scope myapp --topk 5       # cosine search
  kb search "rate limit" --hybrid --query-vec ./qvec.json --scope myapp --topk 5
  kb lint --scope myapp --llm
  kb lint --scope myapp --normalize-categories          # Dry run: report clusters
  kb lint --scope myapp --normalize-categories --apply  # Rewrite to canonical
  kb watch ./src/ --scope myapp --pattern "*.go"

Agent mode (no API key needed):
  kb prepare ./src --scope myapp --pattern "*.go"   # Get prompts
  echo '<compiled JSON>' | kb accept --scope myapp   # Feed results

Environment:
  ANTHROPIC_API_KEY      Required for build/ingest/recompile and --llm lint
                         Not needed for prepare/accept (agent mode) or
                         ingest --article-json (external compilation)
`)

}
