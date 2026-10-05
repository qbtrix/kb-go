// Implements `kb list`.

package cli

import (
	"fmt"

	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func cmdList(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	articles, _ := store.ListArticles(scope)
	if len(articles) == 0 {
		if jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("No articles in knowledge base.")
		}
		return
	}

	if jsonOut {
		var out []map[string]any
		for _, a := range articles {
			out = append(out, map[string]any{
				"id":            a.ID,
				"title":         a.Title,
				"summary":       textutil.Truncate(a.Summary, 120),
				"word_count":    a.WordCount,
				"compiled_with": a.CompiledWith,
				"version":       a.Version,
			})
		}
		printJSON(out)
	} else {
		fmt.Printf("Articles (%d):\n\n", len(articles))
		for _, a := range articles {
			fmt.Printf("  [%s] %s\n", a.ID, a.Title)
			fmt.Printf("    %s\n", textutil.Truncate(a.Summary, 100))
			fmt.Printf("    Words: %d | Version: %d | Compiled: %s\n\n", a.WordCount, a.Version, a.CompiledWith)
		}
	}
}
