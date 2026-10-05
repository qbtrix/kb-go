// Implements `kb recompile`: re-reads an article's raw docs and compiles them
// again through the configured compile path (built-in client or --compiler
// hook; exit 2 without either). A failed compile leaves that article
// untouched and makes the command exit 1.

package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/search"
	"github.com/qbtrix/kb-go/internal/store"
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
		all, _ := store.ListArticles(scope)
		targets = all
	} else {
		a, err := store.LoadArticle(scope, args[0])
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
			raw, err := store.LoadRawDoc(scope, docID)
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

		newArticle, err := compile.Article(spec, combined, source, "", terse)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: recompile failed for %s: %v\n", a.ID, err)
			failed++
			continue
		}

		newArticle.ID = a.ID
		newArticle.Version = a.Version + 1
		newArticle.SourcePath = a.SourcePath
		newArticle.SourceDocs = a.SourceDocs
		store.SaveArticle(scope, newArticle)
		recompiled++
	}

	// Rebuild index
	allArticles, _ := store.ListArticles(scope)
	idx := store.RebuildIndex(scope, allArticles)
	store.SaveIndex(scope, idx)
	search.SaveIndex(scope, search.BuildIndex(allArticles))

	if jsonOut {
		printJSON(map[string]any{"recompiled": recompiled, "failed": failed, "total": len(targets)})
	} else {
		fmt.Printf("Recompiled: %d / %d articles\n", recompiled, len(targets))
	}
	if failed > 0 {
		os.Exit(1)
	}
}
