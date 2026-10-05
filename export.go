// Wiki export: writes a scope's articles and an index page out as a browsable
// markdown wiki.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func exportWiki(scope, outputDir string) {
	articles, _ := listArticles(scope)
	idx := loadIndex(scope)

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
				if contains(a.Categories, cat) {
					fmt.Fprintf(&sb, "- [%s](%s.md) — %s\n", a.Title, a.ID, truncate(a.Summary, 80))
				}
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("## Articles\n\n")
		for _, a := range articles {
			fmt.Fprintf(&sb, "- [%s](%s.md) — %s\n", a.Title, a.ID, truncate(a.Summary, 80))
		}
	}
	os.WriteFile(filepath.Join(outputDir, "index.md"), []byte(sb.String()), 0o644)

	fmt.Printf("Exported %d articles + index.md to %s/\n", len(articles), outputDir)
}
