// Implements `kb search`: BM25 (plain, --context excerpts, --exclude-tags,
// multi-scope), pure vector (--query-vec) and hybrid (--hybrid) modes, and the
// printer for vector/hybrid results.

package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
	"github.com/qbtrix/kb-go/internal/vector"
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
		queryVec, err := vector.LoadFile(queryVecPath)
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
	scopes := store.ResolveScopes(scope)

	// Collect articles from all scopes
	type scopedArticle struct {
		article *model.WikiArticle
		scope   string
	}
	var allArticles []*model.WikiArticle
	var scopeMap []string // parallel array: scope per article
	for _, s := range scopes {
		articles, err := store.ListArticles(s)
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
		var filtered []*model.WikiArticle
		var filteredScopes []string
		for i, a := range allArticles {
			skip := false
			for _, tag := range excluded {
				tag = strings.TrimSpace(tag)
				if slices.Contains(a.Categories, tag) {
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
	var results []*model.WikiArticle
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
	resultScope := func(a *model.WikiArticle) string {
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
			fmt.Printf("     %s\n", textutil.Truncate(a.Summary, 120))
			if len(a.Concepts) > 0 {
				fmt.Printf("     Concepts: %s\n", strings.Join(a.Concepts[:min(len(a.Concepts), 5)], ", "))
			}
			fmt.Println()
		}
	}
}

// emitVectorResults renders vector / hybrid search hits to stdout. Mirrors
// the existing JSON-vs-table split in cmdSearch but with the extended row
// shape (score / bm25_rank / vec_rank / fused_rank). Only called when
// --query-vec is set, so the BM25-only output path is left intact upstream.
//
// Hybrid mode emits all four rank-related keys; pure-vec mode emits only
// `score` and `vec_rank`. We deliberately do NOT add bm25_rank=-1 to pure-vec
// rows — keeping the schema minimal makes consumers easier to write.
func emitVectorResults(results []vectorSearchResult, hybridMode, jsonOut bool) {
	if jsonOut {
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			row := map[string]any{
				"id":       r.Article.ID,
				"title":    r.Article.Title,
				"summary":  r.Article.Summary,
				"concepts": r.Article.Concepts,
				"score":    r.Score,
			}
			if hybridMode {
				row["bm25_rank"] = r.BM25Rank
				row["vec_rank"] = r.VecRank
				row["fused_rank"] = r.FusedRank
			} else {
				row["vec_rank"] = r.VecRank
			}
			out = append(out, row)
		}
		printJSON(out)
		return
	}
	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}
	mode := "vector"
	if hybridMode {
		mode = "hybrid (BM25 + cosine, RRF k=60)"
	}
	fmt.Printf("Found %d results (%s):\n\n", len(results), mode)
	for i, r := range results {
		fmt.Printf("  %d. %s  [score=%.4f", i+1, r.Article.Title, r.Score)
		if hybridMode {
			fmt.Printf(" bm25=%d vec=%d", r.BM25Rank, r.VecRank)
		} else {
			fmt.Printf(" vec=%d", r.VecRank)
		}
		fmt.Println("]")
		if r.Article.Summary != "" {
			fmt.Printf("     %s\n", textutil.Truncate(r.Article.Summary, 120))
		}
		fmt.Println()
	}
}
