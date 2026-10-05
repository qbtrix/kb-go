// Implements `kb serve`: runs the read-only MCP server on stdio for a scope.

package main

import (
	"fmt"
	"io"
	"os"

	"github.com/qbtrix/kb-go/internal/mcp"
)

func cmdServe(args []string) {
	// --scope sets a default scope applied when a tool call omits one. Keeps
	// single-tenant agents from repeating the scope on every call.
	defaultScope := flagStr(args, "--scope", "default")

	srv := mcp.NewServer(os.Stdin, os.Stdout, defaultScope)
	if err := srv.Serve(); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error: serve: %v\n", err)
		os.Exit(1)
	}
}
