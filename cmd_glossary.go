// Implements `kb glossary {list,show,validate}`: argument parsing and exit codes
// over the glossary package functions.

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/qbtrix/kb-go/internal/glossary"
)

func glossaryUsage() {
	fmt.Fprintln(os.Stderr, `Usage: kb glossary <sub> [options]

Sub-commands:
  list                    List all glossary entries in the scope
  show <term>             Print a glossary entry's body
  validate                Check duplicates, alias collisions, dangling refs, contradictions

Flags:
  --scope NAME            Knowledge scope (default: "default")`)
}

// cmdGlossary is the top-level CLI dispatcher for `kb glossary ...`.
func cmdGlossary(args []string) {
	if len(args) < 1 {
		glossaryUsage()
		os.Exit(1)
	}
	sub := args[0]
	rest := args[1:]
	scope := flagStr(rest, "--scope", "default")

	switch sub {
	case "list":
		if err := glossary.List(scope, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "glossary list:", err)
			os.Exit(1)
		}
	case "show":
		// First positional arg is the term. Skip flags and the value
		// that follows a value-taking flag, so `--scope <name>` can
		// appear before or after the term without being read as the term.
		term := ""
		skipNext := false
		for _, a := range rest {
			if skipNext {
				skipNext = false
				continue
			}
			if a == "--scope" {
				skipNext = true
				continue
			}
			if strings.HasPrefix(a, "--") {
				continue
			}
			term = a
			break
		}
		if term == "" {
			fmt.Fprintln(os.Stderr, "usage: kb glossary show <term> [--scope <scope>]")
			os.Exit(1)
		}
		if err := glossary.Show(scope, term, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "glossary show:", err)
			os.Exit(1)
		}
	case "validate":
		issues, err := glossary.Validate(scope)
		if err != nil {
			fmt.Fprintln(os.Stderr, "glossary validate:", err)
			os.Exit(1)
		}
		if len(issues) == 0 {
			fmt.Fprintln(os.Stdout, "OK — glossary is clean.")
			return
		}
		for _, iss := range issues {
			fmt.Fprintln(os.Stdout, iss)
		}
		os.Exit(2)
	case "help", "--help", "-h":
		glossaryUsage()
	default:
		glossaryUsage()
		os.Exit(1)
	}
}
