// Implements `kb search`.

package main

import (
	"fmt"
	"strings"
)

func cmdSearch(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb search <query> [--scope NAME] [--limit N] [--context [--context-chars N] [--context-total N]] [--exclude-tags TAG] [--query-vec PATH] [--hybrid] [--topk N]")
	}

	// First non-flag arg is the text query. May be empty in pure vector mode.
	// (We can't just take args[0] anymore because `kb search --query-vec ...`
	// has no positional text arg.)
	query := ""
	for _, a := range args {
		if !strings.HasPrefix(a, "--") {
			query = a
			break
		}
	}
	scope := flagStr(args, "--scope", "default")
	limit := flagInt(args, "--limit", 5)
	jsonOut := flagBool(args, "--json")
	contextMode := flagBool(args, "--context")
	contextChars := flagInt(args, "--context-chars", defaultContextChars)
	contextTotal := flagInt(args, "--context-total", defaultContextTotal)
	excludeTags := flagStr(args, "--exclude-tags", "")
	queryVecPath := flagStr(args, "--query-vec", "")
	hybridMode := flagBool(args, "--hybrid")
	topK := flagInt(args, "--topk", limit)

	// Vector / hybrid path. When --query-vec is set we route here and skip
	// the existing BM25-only code below — keeping the BM25-only path
	// byte-identical preserves its JSON output shape (no new keys for
	// existing consumers).
	if queryVecPath != "" {
		// Multi-scope vector search isn't in scope yet (see brief). Reject
		// instead of silently picking a default — wrong result silently is
		// worse than a clear error.
		if scope == "*" || strings.Contains(scope, ",") {
			fatal("vector search requires a single --scope, got %q", scope)
		}
		queryVec, err := loadVectorFromFile(queryVecPath)
		if err != nil {
			fatal("load query vector: %v", err)
		}
		var results []vectorSearchResult
		if hybridMode {
			if query == "" {
				fatal("--hybrid requires a text query alongside --query-vec")
			}
			results, err = runHybridSearch(scope, query, queryVec, topK)
		} else {
			results, err = runVectorSearch(scope, queryVec, topK)
		}
		if err != nil {
			fatal("%v", err)
		}
		emitVectorResults(results, hybridMode, jsonOut)
		return
	}

	// Resolve scopes: "*" = all, "a,b,c" = specific, else single
	scopes := resolveScopes(scope)

	// Collect articles from all scopes
	type scopedArticle struct {
		article *WikiArticle
		scope   string
	}
	var allArticles []*WikiArticle
	var scopeMap []string // parallel array: scope per article
	for _, s := range scopes {
		articles, err := listArticles(s)
		if err != nil {
			continue
		}
		for _, a := range articles {
			allArticles = append(allArticles, a)
			scopeMap = append(scopeMap, s)
		}
	}

	// Filter by excluded tags
	if excludeTags != "" {
		excluded := strings.Split(excludeTags, ",")
		var filtered []*WikiArticle
		var filteredScopes []string
		for i, a := range allArticles {
			skip := false
			for _, tag := range excluded {
				tag = strings.TrimSpace(tag)
				if contains(a.Categories, tag) {
					skip = true
					break
				}
			}
			if !skip {
				filtered = append(filtered, a)
				filteredScopes = append(filteredScopes, scopeMap[i])
			}
		}
		allArticles = filtered
		scopeMap = filteredScopes
	}

	// Search with the inverted index (only works for single scope)
	var results []*WikiArticle
	if len(scopes) == 1 {
		var si *SearchIndex
		if excludeTags == "" {
			// Full-scope search: self-heal a missing/stale/old-format index
			// so the next search takes the fast path (best-effort write).
			si = loadOrHealSearchIndex(scopes[0], allArticles)
		} else {
			// Tag-filtered slice — the full-scope index can't match it, so
			// this runs the slow path and must not overwrite the index.
			si = loadSearchIndex(scopes[0])
		}
		results = bm25SearchWithIndex(allArticles, query, limit, si)
	} else {
		results = bm25Search(allArticles, query, limit)
	}

	if contextMode {
		// Output formatted context for agent prompt injection. --json gives
		// the same excerpts as an array, free of the in-band text separator.
		if jsonOut {
			printJSON(searchContextJSON(results, query, contextChars, contextTotal))
			return
		}
		fmt.Print(formatSearchContext(results, query, contextChars, contextTotal))
		return
	}

	// Build a result-to-scope lookup for multi-scope display
	resultScope := func(a *WikiArticle) string {
		for i, art := range allArticles {
			if art == a && i < len(scopeMap) {
				return scopeMap[i]
			}
		}
		return ""
	}
	multiScope := len(scopes) > 1

	if jsonOut {
		out := make([]map[string]any, 0, len(results))
		for _, a := range results {
			entry := map[string]any{
				"id":       a.ID,
				"title":    a.Title,
				"summary":  a.Summary,
				"concepts": a.Concepts,
			}
			if multiScope {
				entry["scope"] = resultScope(a)
			}
			out = append(out, entry)
		}
		printJSON(out)
	} else {
		if len(results) == 0 {
			fmt.Println("No results found.")
			return
		}
		fmt.Printf("Found %d results:\n\n", len(results))
		for i, a := range results {
			scopeLabel := ""
			if multiScope {
				scopeLabel = fmt.Sprintf(" [%s]", resultScope(a))
			}
			fmt.Printf("  %d. %s%s\n", i+1, a.Title, scopeLabel)
			fmt.Printf("     %s\n", truncate(a.Summary, 120))
			if len(a.Concepts) > 0 {
				fmt.Printf("     Concepts: %s\n", strings.Join(a.Concepts[:min(len(a.Concepts), 5)], ", "))
			}
			fmt.Println()
		}
	}
}
