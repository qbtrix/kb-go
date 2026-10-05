// Implements `kb recompile`.

package main

import (
	"fmt"
	"os"
	"strings"
)

func cmdRecompile(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb recompile <article_id|--all> [--scope NAME] [--model MODEL]")
	}

	scope := flagStr(args, "--scope", "default")
	model := flagStr(args, "--model", defaultModel)
	jsonOut := flagBool(args, "--json")
	terse := flagBool(args, "--terse")
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	recompileAll := flagBool(args, "--all") || args[0] == "--all"

	var targets []*WikiArticle
	if recompileAll {
		all, _ := listArticles(scope)
		targets = all
	} else {
		a, err := loadArticle(scope, args[0])
		if err != nil || a == nil {
			fatal("Article not found: %s", args[0])
		}
		targets = []*WikiArticle{a}
	}

	var recompiled int
	for _, a := range targets {
		// Load raw source docs
		var texts []string
		for _, docID := range a.SourceDocs {
			raw, err := loadRawDoc(scope, docID)
			if err == nil && raw != nil {
				texts = append(texts, raw.RawText)
			}
		}
		if len(texts) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: no raw docs for %s, skipping\n", a.ID)
			continue
		}

		combined := strings.Join(texts, "\n\n")
		source := "recompile:" + a.ID

		if !jsonOut {
			fmt.Printf("Recompiling: %s\n", a.Title)
		}

		newArticle, _, err := compileLLM(combined, source, model, apiKey, nil, terse)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: recompilation failed for %s: %v\n", a.ID, err)
			continue
		}

		newArticle.ID = a.ID
		newArticle.Version = a.Version + 1
		newArticle.SourcePath = a.SourcePath
		newArticle.SourceDocs = a.SourceDocs
		saveArticle(scope, newArticle)
		recompiled++
	}

	// Rebuild index
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	if jsonOut {
		printJSON(map[string]any{"recompiled": recompiled, "total": len(targets)})
	} else {
		fmt.Printf("Recompiled: %d / %d articles\n", recompiled, len(targets))
	}
}
