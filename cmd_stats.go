// Implements `kb stats`.

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func cmdStats(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	articles, _ := listArticles(scope)
	idx := loadIndex(scope)
	rawCount := 0
	rawDir := filepath.Join(scopeDir(scope), "raw")
	if entries, err := os.ReadDir(rawDir); err == nil {
		rawCount = len(entries)
	}
	totalWords := 0
	for _, a := range articles {
		totalWords += a.WordCount
	}
	vectorCount := vectorIndexCount(scope)

	if jsonOut {
		printJSON(map[string]any{
			"scope":      scope,
			"articles":   len(articles),
			"raw_docs":   rawCount,
			"words":      totalWords,
			"concepts":   len(idx.Concepts),
			"categories": len(idx.Categories),
			"vectors":    vectorCount,
		})
	} else {
		fmt.Printf("Knowledge Base: %s\n", scope)
		fmt.Printf("  Articles:   %d\n", len(articles))
		fmt.Printf("  Raw docs:   %d\n", rawCount)
		fmt.Printf("  Words:      %d\n", totalWords)
		fmt.Printf("  Concepts:   %d\n", len(idx.Concepts))
		fmt.Printf("  Categories: %d\n", len(idx.Categories))
		fmt.Printf("  Vectors:    %d\n", vectorCount)
	}
}
