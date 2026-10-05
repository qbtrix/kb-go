// Implements `kb show`.

package cli

import (
	"fmt"
	"strings"

	"github.com/qbtrix/kb-go/internal/store"
)

func cmdShow(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb show <article_id> [--scope NAME]")
	}
	id := args[0]
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	a, err := store.LoadArticle(scope, id)
	if err != nil || a == nil {
		fatal("Article not found: %s", id)
	}

	if jsonOut {
		printJSON(map[string]any{
			"id":            a.ID,
			"title":         a.Title,
			"summary":       a.Summary,
			"content":       a.Content,
			"concepts":      a.Concepts,
			"categories":    a.Categories,
			"backlinks":     a.Backlinks,
			"word_count":    a.WordCount,
			"compiled_with": a.CompiledWith,
			"version":       a.Version,
		})
	} else {
		fmt.Printf("# %s\n", a.Title)
		fmt.Printf("ID: %s | Version: %d | Words: %d\n", a.ID, a.Version, a.WordCount)
		if len(a.Concepts) > 0 {
			fmt.Printf("Concepts: %s\n", strings.Join(a.Concepts, ", "))
		}
		if len(a.Categories) > 0 {
			fmt.Printf("Categories: %s\n", strings.Join(a.Categories, ", "))
		}
		fmt.Printf("Compiled with: %s\n", a.CompiledWith)
		fmt.Print("\n---\n\n")
		fmt.Println(a.Content)
	}
}
