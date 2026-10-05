// Implements `kb graph`: flag parsing, mode selection and output format over the
// graph core in graph.go.

package main

import (
	"fmt"
)

// cmdGraph exports the concept graph in a portable format.
// Default view: top N concepts by article count, with edges between
// concepts that share at least one article. Use --concept to focus on
// a single concept's neighborhood, or --article for an article's concepts.
func cmdGraph(args []string) {
	scope := flagStr(args, "--scope", "default")
	format := flagStr(args, "--format", "mermaid")
	focusConcept := flagStr(args, "--concept", "")
	focusArticle := flagStr(args, "--article", "")
	limit := flagInt(args, "--limit", 30)
	minArticles := flagInt(args, "--min-articles", 2)

	idx := loadIndex(scope)
	if len(idx.Concepts) == 0 {
		fatal("No concepts found in scope %s", scope)
	}

	// Build the graph based on mode
	var nodes []graphNode
	var edges []graphEdge
	var err error

	switch {
	case focusConcept != "":
		nodes, edges, err = buildConceptSubgraph(idx, focusConcept)
	case focusArticle != "":
		nodes, edges, err = buildArticleSubgraph(idx, focusArticle)
	default:
		nodes, edges = buildConceptGraph(idx, limit, minArticles)
	}
	if err != nil {
		fatal("%v", err)
	}

	if len(nodes) == 0 {
		fatal("Graph is empty. Try --limit higher or --min-articles lower.")
	}

	switch format {
	case "mermaid":
		fmt.Print(renderMermaid(nodes, edges, focusConcept))
	case "json":
		printJSON(map[string]any{
			"scope": scope,
			"nodes": nodes,
			"edges": edges,
		})
	case "dot":
		fmt.Print(renderDot(nodes, edges, focusConcept))
	default:
		fatal("Unknown format: %s (expected mermaid, json, or dot)", format)
	}
}
