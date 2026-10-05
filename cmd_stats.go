// Implements `kb stats`. When any article carries a usage record, the output
// adds compile-spend totals (articles with usage, input/output tokens, cost);
// otherwise the output is unchanged.

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/qbtrix/kb-go/internal/compile"
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
	usageN, usageIn, usageOut, usageCost := compile.UsageTotals(articles)

	if jsonOut {
		out := map[string]any{
			"scope":      scope,
			"articles":   len(articles),
			"raw_docs":   rawCount,
			"words":      totalWords,
			"concepts":   len(idx.Concepts),
			"categories": len(idx.Categories),
			"vectors":    vectorCount,
		}
		if usageN > 0 {
			out["usage"] = map[string]any{
				"articles":      usageN,
				"input_tokens":  usageIn,
				"output_tokens": usageOut,
				"cost_usd":      usageCost,
			}
		}
		printJSON(out)
	} else {
		fmt.Printf("Knowledge Base: %s\n", scope)
		fmt.Printf("  Articles:   %d\n", len(articles))
		fmt.Printf("  Raw docs:   %d\n", rawCount)
		fmt.Printf("  Words:      %d\n", totalWords)
		fmt.Printf("  Concepts:   %d\n", len(idx.Concepts))
		fmt.Printf("  Categories: %d\n", len(idx.Categories))
		fmt.Printf("  Vectors:    %d\n", vectorCount)
		if usageN > 0 {
			cost := ""
			if usageCost > 0 {
				cost = fmt.Sprintf(", $%.4f", usageCost)
			}
			fmt.Printf("  Compile usage (%d articles): %d input + %d output tokens%s\n", usageN, usageIn, usageOut, cost)
		}
	}
}
