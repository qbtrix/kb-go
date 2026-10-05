// kb is a headless knowledge-base CLI: it stores LLM-written wiki articles
// compiled from source files and docs, then answers queries with BM25 search
// over them. kb holds no LLM client: the caller owns the model, either through
// a --compiler command kb pipes prompts to, or by compiling in its own agent
// (`kb prepare` -> `kb accept`, `kb ingest --article-json`).
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
