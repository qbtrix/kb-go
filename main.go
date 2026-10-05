// kb is a headless knowledge-base CLI: it stores LLM-written wiki articles
// compiled from source files and docs, then answers queries with BM25 search
// over them. kb holds no LLM client: the caller owns the model, either through
// a --compiler command kb pipes prompts to, or by compiling in its own agent
// (`kb prepare` → `kb accept`, `kb ingest --article-json`).
// This file holds the entry point, command dispatch, usage text and the version
// string. Storage is markdown with JSON frontmatter; the only external dep is
// fsnotify (watch mode).

package main

import (
	"fmt"
	"os"
	"runtime/debug"
)

// formatVersion reports the module version stamped by `go install …@vX`.
// Builds without one (a local `go build`, "(devel)") report "dev", plus the
// VCS revision and a dirty marker when the toolchain recorded them.
func formatVersion(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	var rev string
	dirty := false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 12 {
		rev = rev[:12]
	}
	if dirty {
		return "dev (" + rev + ", dirty)"
	}
	return "dev (" + rev + ")"
}

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
		fmt.Println("kb " + formatVersion(debug.ReadBuildInfo()))
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
  build <path>           Scan files, compile pending ones with --compiler, build KB
  prepare <path>         Output compilation prompts as JSON (agent mode)
  accept                 Read compiled articles from stdin (agent mode companion)
  graph                  Export concept graph (mermaid, dot, or json)
  search <query>         BM25 search over compiled articles
  ingest [file]          Ingest a file or stdin text, compiled with --compiler.
                         Fails loudly if the compile fails (raw doc kept, no
                         article). Flags: --article-json (read
                         {"raw_text","article"} from stdin — the caller already
                         compiled it), --allow-fallback (store the text
                         verbatim when there is no compiler or it fails)
  show <article_id>      Show a full article
  list                   List all articles
  stats                  Show KB statistics
  lint                   Structural health check. Flags: --llm (LLM review
                         via --compiler), --normalize-categories [--apply] (collapse
                         variant labels like "CLI"/"cli" to a canonical form)
  recompile <id|--all>   Recompile article(s) from raw source with --compiler
  delete <article_id>    Remove one article from a scope (files, index,
                         concept graph, caches). Idempotent.
  clear                  Delete all knowledge for a scope
  watch <path>           Auto-rebuild on file changes (needs --compiler)
  convo <sub>            Conversation mode (ingest, search, list)
  serve                  Expose read-only KB tools over MCP on stdio
  glossary <sub>         Domain glossary (list, show, validate)
  version                Show version

Global flags:
  --scope NAME           Knowledge scope (default: "default")
  --json                 Output as JSON (for machine consumption)
  --compiler "CMD"       Command that compiles one article: kb writes the prompt
                         to its stdin, reads one JSON article from its stdout.
                         Runs via sh -c (cmd /S /C on Windows). Env: KB_COMPILER
  --compiler-timeout D   Per-item compiler timeout (default 300s; "90s", "2m", 45)
  --concurrency N        Parallel compiler runs (default: 5, build only)
  --pattern GLOB         File patterns, comma-separated (e.g. "*.go,*.py,*.ts")
  --lang LANG            Language hint for stdin ingest (go, python, typescript)

Examples:
  kb build ./src/myapp --scope myapp --compiler "python examples/compilers/openai_compatible.py"
  kb search "auth middleware" --scope myapp
  kb search "shoe sizes" --scope shop --context --context-chars 2000   # prompt-ready excerpts
  kb ingest ./README.md --scope myapp --compiler "$KB_COMPILER"
  echo "some text" | kb ingest --scope myapp --source "notes" --allow-fallback
  kb ingest --vec ./vec.json --id article-1 --scope myapp        # attach embedding
  kb search --query-vec ./qvec.json --scope myapp --topk 5       # cosine search
  kb search "rate limit" --hybrid --query-vec ./qvec.json --scope myapp --topk 5
  kb lint --scope myapp --llm
  kb lint --scope myapp --normalize-categories          # Dry run: report clusters
  kb lint --scope myapp --normalize-categories --apply  # Rewrite to canonical
  kb watch ./src/ --scope myapp --pattern "*.go" --compiler "$KB_COMPILER"

Agent mode (your agent is the compiler):
  kb prepare ./src --scope myapp --pattern "*.go"   # Get prompts
  echo '<compiled JSON>' | kb accept --scope myapp   # Feed results

Compiler output: one JSON object {"title","summary","content","concepts",
"categories"} plus optional "usage": {"model","input_tokens","output_tokens",
"cost_usd"} (also accepted by accept and ingest --article-json; kb stats sums
it). Recipes (headless Claude Code, LiteLLM/OpenAI-compatible, Ollama):
README.md, "Compiling articles".

Environment:
  KB_COMPILER            Default for --compiler (the flag wins)
`)

}
