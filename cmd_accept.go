// Implements `kb accept`.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"time"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// cmdAccept reads compiled article results from stdin and saves them.
// Companion to `kb prepare` — accepts the agent's compilation output.
// Input: JSON object with "scope" and "articles" array.
// Each article: {"source", "hash", "raw_id", "title", "summary", "content", "concepts", "categories"},
// plus optional "compiled_with" and "usage" ({"model","input_tokens","output_tokens","cost_usd"}).
// compiled_with defaults to usage.model, then "agent". Re-accepting an
// article replaces its usage; it is never summed.
func cmdAccept(args []string) {
	scope := flagStr(args, "--scope", "")

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal("Failed to read stdin: %v", err)
	}

	// Accept either a single object with "articles" array, or a bare array.
	// audience/depth/target_words are optional — set by the agent if compiling
	// from a terse-mode prepare. accept detects terse mode from these fields
	// rather than requiring a separate --terse flag (cleaner: the payload is
	// self-describing, no flag state to thread through multiple hops).
	type acceptArticle struct {
		Source      string   `json:"source"`
		Hash        string   `json:"hash"`
		RawID       string   `json:"raw_id"`
		Title       string   `json:"title"`
		Summary     string   `json:"summary"`
		Content     string   `json:"content"`
		Concepts    []string `json:"concepts"`
		Categories  []string `json:"categories"`
		IsTest      bool     `json:"is_test"`
		Audience    string   `json:"audience,omitempty"`
		Depth       string   `json:"depth,omitempty"`
		TargetWords int      `json:"target_words,omitempty"`
		// Optional: who compiled it and what it cost (see ArticleUsage).
		CompiledWith string          `json:"compiled_with,omitempty"`
		Usage        json.RawMessage `json:"usage,omitempty"`
	}

	var articles []acceptArticle

	// Try wrapped format first: {"scope": "...", "articles": [...]}
	var wrapped struct {
		Scope    string          `json:"scope"`
		Articles []acceptArticle `json:"articles"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Articles) > 0 {
		articles = wrapped.Articles
		if scope == "" {
			scope = wrapped.Scope
		}
	} else {
		// Try bare array
		if err := json.Unmarshal(data, &articles); err != nil {
			// Try single object
			var single acceptArticle
			if err := json.Unmarshal(data, &single); err != nil {
				fatal("Failed to parse input. Expected JSON with articles array, array, or single article object.")
			}
			articles = []acceptArticle{single}
		}
	}

	if scope == "" {
		scope = "default"
	}

	store.EnsureDirs(scope)
	cache := store.LoadCache(scope)
	ids := store.LoadIDRegistry(scope)
	saved := 0

	for _, a := range articles {
		if a.Title == "" || a.Content == "" {
			fmt.Fprintf(os.Stderr, "Warning: skipping article for %s (missing title or content)\n", a.Source)
			continue
		}

		slug := textutil.Slugify(a.Title)
		now := time.Now().UTC().Format(time.RFC3339)

		// Infer terse mode from the depth field in the payload. An agent that
		// compiled from a terse-mode prepare prompt sets depth=overview. This
		// avoids needing a separate --terse flag on accept.
		audience, depth, targetWords := a.Audience, a.Depth, a.TargetWords
		if audience == "" {
			if depth == "overview" {
				audience = "agent"
			} else {
				audience = "human"
			}
		}
		if depth == "" {
			depth = "deep"
		}
		if targetWords == 0 {
			if depth == "overview" {
				targetWords = 150
			} else {
				targetWords = 500
			}
		}

		usage := compile.ParseUsage(a.Usage)
		article := &model.WikiArticle{
			ID:           slug,
			Title:        a.Title,
			Summary:      a.Summary,
			Content:      a.Content,
			Concepts:     textutil.NilToEmpty(a.Concepts),
			Categories:   textutil.NilToEmpty(a.Categories),
			SourcePath:   a.Source,
			SourceDocs:   []string{a.RawID},
			WordCount:    textutil.WordCount(a.Content),
			CompiledAt:   now,
			CompiledWith: compile.CompiledWith(a.CompiledWith, usage, "agent"),
			Version:      1,
			Audience:     audience,
			Depth:        depth,
			TargetWords:  targetWords,
			Usage:        usage,
		}

		// Auto-tag test files
		if a.IsTest && !slices.Contains(article.Categories, "test") {
			article.Categories = append(article.Categories, "test")
		}

		article.ID, article.Version = ids.Claim(article.ID, article.SourcePath, article.SourceDocs, false)
		store.SaveArticle(scope, article)

		// Update cache
		if a.Hash != "" && a.Source != "" {
			cache.Files[a.Source] = model.CacheEntry{
				Hash:       a.Hash,
				ArticleID:  article.ID,
				CompiledAt: now,
			}
		}
		saved++
	}

	ids.Retire(scope)
	store.SaveCache(scope, cache)

	// Rebuild index
	allArticles, _ := store.ListArticles(scope)
	idx := store.RebuildIndex(scope, allArticles)
	store.SaveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	output := map[string]any{
		"accepted": saved,
		"articles": len(allArticles),
		"concepts": len(idx.Concepts),
	}
	printJSON(output)
}
