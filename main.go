// kb is a headless knowledge-base CLI: an LLM compiles source files and docs
// into wiki articles at write time, then kb answers queries with BM25 search
// over them. The default compiler is the built-in Anthropic client
// (ANTHROPIC_API_KEY); callers can bring their own model through a --compiler
// command, or compile in their own agent (`kb prepare` -> `kb accept`,
// `kb ingest --article-json`).
//
// This file is the whole binary entry point; the command layer is
// internal/cli and the library is the rest of internal/. It stays in the
// module root so `go install github.com/qbtrix/kb-go@vX` keeps building a
// binary named kb-go.
package main

import (
	"os"

	"github.com/qbtrix/kb-go/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
