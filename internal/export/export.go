// Package export renders a scope for outside consumers: Wiki writes the
// articles and an index page out as a browsable markdown wiki, and graph.go
// builds the concept graph behind `kb graph` (the whole-scope graph, one
// concept's or article's neighbourhood) with mermaid and dot renderers. The
// graph builders return errors instead of exiting; the command prints them.
package export

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func Wiki(scope, outputDir string) {
	articles, _ := store.ListArticles(scope)
	idx := store.LoadIndex(scope)

	os.MkdirAll(outputDir, 0o755)

	// Write each article as a standalone .md
	for _, a := range articles {
		var sb strings.Builder
		fmt.Fprintf(&sb, "# %s\n\n", a.Title)
		if a.Summary != "" {
			fmt.Fprintf(&sb, "> %s\n\n", a.Summary)
		}
		if len(a.Categories) > 0 {
			fmt.Fprintf(&sb, "**Categories:** %s  \n", strings.Join(a.Categories, ", "))
		}
		if len(a.Concepts) > 0 {
			top := a.Concepts
			if len(top) > 10 {
				top = top[:10]
			}
			fmt.Fprintf(&sb, "**Concepts:** %s  \n", strings.Join(top, ", "))
		}
		fmt.Fprintf(&sb, "**Words:** %d | **Version:** %d\n\n---\n\n", a.WordCount, a.Version)
		sb.WriteString(a.Content)
		if len(a.Backlinks) > 0 {
			sb.WriteString("\n\n---\n\n## Related\n\n")
			for _, link := range a.Backlinks {
				fmt.Fprintf(&sb, "- [%s](%s.md)\n", link, link)
			}
		}
		os.WriteFile(filepath.Join(outputDir, a.ID+".md"), []byte(sb.String()), 0o644)
	}

	// Write index.md
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Knowledge Base: %s\n\n", scope)
	fmt.Fprintf(&sb, "**%d articles** | **%d concepts** | **%d categories**\n\n", len(articles), len(idx.Concepts), len(idx.Categories))

	if len(idx.Categories) > 0 {
		sb.WriteString("## Categories\n\n")
		for _, cat := range idx.Categories {
			fmt.Fprintf(&sb, "### %s\n\n", cat)
			for _, a := range articles {
				if slices.Contains(a.Categories, cat) {
					fmt.Fprintf(&sb, "- [%s](%s.md) — %s\n", a.Title, a.ID, textutil.Truncate(a.Summary, 80))
				}
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("## Articles\n\n")
		for _, a := range articles {
			fmt.Fprintf(&sb, "- [%s](%s.md) — %s\n", a.Title, a.ID, textutil.Truncate(a.Summary, 80))
		}
	}
	os.WriteFile(filepath.Join(outputDir, "index.md"), []byte(sb.String()), 0o644)

	fmt.Printf("Exported %d articles + index.md to %s/\n", len(articles), outputDir)
}
