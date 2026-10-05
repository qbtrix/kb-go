// Concept-graph export core for `kb graph`: the whole-scope concept graph
// (top concepts by article count, edges between concepts that share articles),
// the one-hop neighbourhood of a concept or an article, and the mermaid and dot
// renderers. The builders return errors instead of exiting; the command prints
// them.

package main

import (
	"fmt"
	"sort"
	"strings"
)

type graphNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // "concept" or "article"
	Size  int    `json:"size"` // article count for concepts, concept count for articles
}

type graphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Weight int    `json:"weight"` // number of shared articles
}

// buildConceptGraph returns top N concepts + edges for co-occurring concepts.
func buildConceptGraph(idx *KnowledgeIndex, limit, minArticles int) ([]graphNode, []graphEdge) {
	// Collect concepts with >= minArticles, sort by article count desc
	type conceptStat struct {
		name     string
		articles []string
	}
	var stats []conceptStat
	for name, c := range idx.Concepts {
		if len(c.Articles) >= minArticles {
			stats = append(stats, conceptStat{name: name, articles: c.Articles})
		}
	}
	sort.Slice(stats, func(i, j int) bool {
		return len(stats[i].articles) > len(stats[j].articles)
	})
	if len(stats) > limit {
		stats = stats[:limit]
	}

	// Build nodes
	nodes := make([]graphNode, 0, len(stats))
	included := make(map[string]bool)
	for i, s := range stats {
		id := fmt.Sprintf("c%d", i)
		nodes = append(nodes, graphNode{
			ID:    id,
			Label: s.name,
			Kind:  "concept",
			Size:  len(s.articles),
		})
		included[s.name] = true
	}

	// Build edges between co-occurring concepts
	// For each pair of concepts, count shared articles
	nameToID := make(map[string]string, len(nodes))
	for i, n := range nodes {
		nameToID[n.Label] = fmt.Sprintf("c%d", i)
	}

	var edges []graphEdge
	for i := 0; i < len(stats); i++ {
		for j := i + 1; j < len(stats); j++ {
			shared := countSharedArticles(stats[i].articles, stats[j].articles)
			if shared > 0 {
				edges = append(edges, graphEdge{
					Source: nameToID[stats[i].name],
					Target: nameToID[stats[j].name],
					Weight: shared,
				})
			}
		}
	}
	return nodes, edges
}

// buildConceptSubgraph returns nodes and edges for a one-hop neighborhood
// around a focus concept: the concept, its articles, and other concepts
// those articles contain. Errors when the concept is not in the index.
func buildConceptSubgraph(idx *KnowledgeIndex, focus string) ([]graphNode, []graphEdge, error) {
	c, ok := idx.Concepts[focus]
	if !ok {
		// Case-insensitive fallback
		for name, cc := range idx.Concepts {
			if strings.EqualFold(name, focus) {
				c = cc
				focus = name
				ok = true
				break
			}
		}
	}
	if !ok {
		return nil, nil, fmt.Errorf("Concept not found: %s", focus)
	}

	nodes := []graphNode{{ID: "focus", Label: focus, Kind: "concept", Size: len(c.Articles)}}
	articleNodes := make(map[string]string) // articleID -> nodeID
	for i, articleID := range c.Articles {
		nodeID := fmt.Sprintf("a%d", i)
		label := articleID
		if a, exists := idx.Articles[articleID]; exists {
			if m, isMap := a.(map[string]any); isMap {
				if t, hasTitle := m["title"].(string); hasTitle {
					label = t
				}
			}
		}
		nodes = append(nodes, graphNode{
			ID:    nodeID,
			Label: label,
			Kind:  "article",
			Size:  1,
		})
		articleNodes[articleID] = nodeID
	}

	edges := make([]graphEdge, 0, len(c.Articles))
	for _, nodeID := range articleNodes {
		edges = append(edges, graphEdge{Source: "focus", Target: nodeID, Weight: 1})
	}

	// Find related concepts (other concepts appearing in the same articles)
	related := make(map[string]int)
	articleSet := make(map[string]bool)
	for _, a := range c.Articles {
		articleSet[a] = true
	}
	for name, cc := range idx.Concepts {
		if name == focus {
			continue
		}
		overlap := 0
		for _, a := range cc.Articles {
			if articleSet[a] {
				overlap++
			}
		}
		if overlap > 0 {
			related[name] = overlap
		}
	}
	// Sort related concepts by overlap desc, take top 10
	type relatedStat struct {
		name    string
		overlap int
	}
	var relStats []relatedStat
	for name, overlap := range related {
		relStats = append(relStats, relatedStat{name: name, overlap: overlap})
	}
	sort.Slice(relStats, func(i, j int) bool {
		return relStats[i].overlap > relStats[j].overlap
	})
	if len(relStats) > 10 {
		relStats = relStats[:10]
	}
	for i, r := range relStats {
		nodeID := fmt.Sprintf("r%d", i)
		nodes = append(nodes, graphNode{
			ID:    nodeID,
			Label: r.name,
			Kind:  "concept",
			Size:  r.overlap,
		})
		edges = append(edges, graphEdge{Source: "focus", Target: nodeID, Weight: r.overlap})
	}

	return nodes, edges, nil
}

// buildArticleSubgraph returns nodes for an article and its concepts.
// Errors when the article cannot be loaded.
func buildArticleSubgraph(idx *KnowledgeIndex, articleID string) ([]graphNode, []graphEdge, error) {
	article, err := loadArticle(idx.Scope, articleID)
	if err != nil || article == nil {
		return nil, nil, fmt.Errorf("Article not found: %s", articleID)
	}

	nodes := []graphNode{{ID: "focus", Label: article.Title, Kind: "article", Size: len(article.Concepts)}}
	edges := make([]graphEdge, 0, len(article.Concepts))
	for i, concept := range article.Concepts {
		nodeID := fmt.Sprintf("c%d", i)
		size := 1
		if c, ok := idx.Concepts[concept]; ok {
			size = len(c.Articles)
		}
		nodes = append(nodes, graphNode{
			ID:    nodeID,
			Label: concept,
			Kind:  "concept",
			Size:  size,
		})
		edges = append(edges, graphEdge{Source: "focus", Target: nodeID, Weight: 1})
	}
	return nodes, edges, nil
}

func countSharedArticles(a, b []string) int {
	set := make(map[string]bool, len(a))
	for _, x := range a {
		set[x] = true
	}
	n := 0
	for _, x := range b {
		if set[x] {
			n++
		}
	}
	return n
}

// renderMermaid produces a Mermaid graph diagram from nodes and edges.
func renderMermaid(nodes []graphNode, edges []graphEdge, focus string) string {
	var sb strings.Builder
	sb.WriteString("graph LR\n")
	for _, n := range nodes {
		label := escapeMermaid(n.Label)
		switch n.Kind {
		case "concept":
			sb.WriteString(fmt.Sprintf("  %s([\"%s\"])\n", n.ID, label))
		case "article":
			sb.WriteString(fmt.Sprintf("  %s[\"%s\"]\n", n.ID, label))
		}
	}
	for _, e := range edges {
		sb.WriteString(fmt.Sprintf("  %s --- %s\n", e.Source, e.Target))
	}
	// Style the focus node if present
	if focus != "" {
		for _, n := range nodes {
			if n.ID == "focus" {
				sb.WriteString(fmt.Sprintf("  style %s fill:#f9a825,stroke:#333,stroke-width:2px\n", n.ID))
				break
			}
		}
	}
	return sb.String()
}

// renderDot produces a Graphviz DOT graph from nodes and edges.
func renderDot(nodes []graphNode, edges []graphEdge, focus string) string {
	var sb strings.Builder
	sb.WriteString("graph G {\n")
	sb.WriteString("  rankdir=LR;\n")
	sb.WriteString("  node [fontname=\"Helvetica\"];\n")
	for _, n := range nodes {
		shape := "ellipse"
		if n.Kind == "article" {
			shape = "box"
		}
		label := strings.ReplaceAll(n.Label, "\"", "\\\"")
		sb.WriteString(fmt.Sprintf("  %s [label=\"%s\", shape=%s];\n", n.ID, label, shape))
	}
	for _, e := range edges {
		sb.WriteString(fmt.Sprintf("  %s -- %s;\n", e.Source, e.Target))
	}
	sb.WriteString("}\n")
	return sb.String()
}

// escapeMermaid escapes characters that would break Mermaid node labels.
func escapeMermaid(s string) string {
	s = strings.ReplaceAll(s, "\"", "'")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
