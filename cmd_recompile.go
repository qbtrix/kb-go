// Implements `kb recompile`: re-reads an article's raw docs and compiles them
// again through the configured compile path (built-in client or --compiler
// hook; exit 2 without either). A failed compile leaves that article
// untouched and makes the command exit 1.

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
)

func cmdRecompile(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb recompile <article_id|--all> [--scope NAME] [--model MODEL | --compiler \"<command>\"]")
	}

	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")
	terse := flagBool(args, "--terse")
	recompileAll := flagBool(args, "--all") || args[0] == "--all"
	spec := mustCompilerFromArgs(args)
	requireCompiler(spec, "recompile", "")

	var targets []*model.WikiArticle
	if recompileAll {
		all, _ := listArticles(scope)
		targets = all
	} else {
		a, err := loadArticle(scope, args[0])
		if err != nil || a == nil {
			fatal("Article not found: %s", args[0])
		}
		targets = []*model.WikiArticle{a}
	}

	var recompiled, failed int
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

		newArticle, err := compileArticle(spec, combined, source, nil, terse)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: recompile failed for %s: %v\n", a.ID, err)
			failed++
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
		printJSON(map[string]any{"recompiled": recompiled, "failed": failed, "total": len(targets)})
	} else {
		fmt.Printf("Recompiled: %d / %d articles\n", recompiled, len(targets))
	}
	if failed > 0 {
		os.Exit(1)
	}
}
