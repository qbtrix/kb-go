// Tests for Tokenize and BM25 (ranking, empty corpus/query, limit), and the
// glossary boost: an exact Term or Alias match outranks a module article that
// mentions the word more often, case-insensitively.

package search

import (
	"testing"

	"github.com/qbtrix/kb-go/internal/model"
)

func TestTokenize(t *testing.T) {
	tokens := Tokenize("Hello, World! This is a TEST-123.")
	// "this" -> "thi": Tokenize stems, and step 1a strips the final s; the
	// <=2-letter guard leaves "is"/"a" and the digit token "123" unchanged.
	expected := []string{"hello", "world", "thi", "is", "a", "test", "123"}
	if len(tokens) != len(expected) {
		t.Fatalf("tokenize got %d tokens, want %d: %v", len(tokens), len(expected), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token[%d] = %q, want %q", i, tok, expected[i])
		}
	}
}

func TestTokenizeEmpty(t *testing.T) {
	tokens := Tokenize("")
	if len(tokens) != 0 {
		t.Errorf("tokenize('') should return empty, got %v", tokens)
	}
}

func TestBM25Search(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Title: "Authentication Guide", Summary: "How to authenticate users with JWT tokens", Content: "JWT auth flow using bearer tokens"},
		{ID: "a2", Title: "Database Setup", Summary: "Setting up PostgreSQL for production", Content: "PostgreSQL configuration and connection pooling"},
		{ID: "a3", Title: "API Gateway", Summary: "Gateway handles auth and routing", Content: "Routes requests and validates JWT tokens"},
	}

	results := BM25(articles, "JWT authentication", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'JWT authentication'")
	}
	// Auth guide should rank first (has both JWT and auth)
	if results[0].ID != "a1" {
		t.Errorf("expected a1 first, got %s", results[0].ID)
	}
}

func TestBM25SearchNoResults(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Title: "Hello", Content: "world"},
	}
	results := BM25(articles, "nonexistent", 5)
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestBM25SearchEmptyQuery(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Title: "Hello", Content: "world"},
	}
	results := BM25(articles, "", 5)
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestBM25SearchEmptyCorpus(t *testing.T) {
	results := BM25(nil, "test", 5)
	if results != nil {
		t.Errorf("expected nil for empty corpus")
	}
}

func TestBM25SearchLimit(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Title: "Go", Summary: "Go language", Content: "Go programming language"},
		{ID: "a2", Title: "Go testing", Summary: "Go tests", Content: "Go test framework"},
		{ID: "a3", Title: "Go modules", Summary: "Go mod", Content: "Go module system"},
	}
	results := BM25(articles, "Go", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

// makeSearchCorpus returns the two-article corpus used by the search boost
// tests: a module article that mentions "pocket" multiple times in its body
// vs a glossary article whose Term == "Pocket" and Aliases include "pkt".
// Without the glossary boost the module article wins (more occurrences).
func makeSearchCorpus() []*model.WikiArticle {
	return []*model.WikiArticle{
		{
			ID:      "pocket-service",
			Title:   "Pocket Service",
			Content: "the pocket service handles routing for the pocket subsystem and integrates with pocket clients",
			Version: 1,
		},
		{
			ID:      "pocket",
			Title:   "Pocket",
			Content: "A Pocket is a workspace container.",
			Kind:    "glossary",
			Term:    "Pocket",
			Aliases: []string{"pkt"},
			Version: 1,
		},
	}
}

// TODO: passes after glossary feature lands
func TestGlossarySearchExactTermBoost(t *testing.T) {
	articles := makeSearchCorpus()
	idx := BuildIndex(articles)
	results := BM25WithIndex(articles, "pocket", 10, idx)

	if len(results) == 0 {
		t.Fatal("no search results")
	}
	if results[0].Kind != "glossary" {
		t.Errorf("first result Kind = %q, want %q (glossary article should rank first via exact-term boost). Order: %v",
			results[0].Kind, "glossary", articleIDs(results))
	}
	if results[0].Term != "Pocket" {
		t.Errorf("first result Term = %q, want %q", results[0].Term, "Pocket")
	}
}

// TODO: passes after glossary feature lands
func TestGlossarySearchAliasBoost(t *testing.T) {
	articles := makeSearchCorpus()
	idx := BuildIndex(articles)
	results := BM25WithIndex(articles, "pkt", 10, idx)

	if len(results) == 0 {
		t.Fatal("no search results for query 'pkt'")
	}
	if results[0].Kind != "glossary" {
		t.Errorf("first result Kind = %q, want %q (glossary article should match via alias). Order: %v",
			results[0].Kind, "glossary", articleIDs(results))
	}
}

// TODO: passes after glossary feature lands
func TestGlossarySearchCaseInsensitive(t *testing.T) {
	articles := makeSearchCorpus()
	idx := BuildIndex(articles)
	results := BM25WithIndex(articles, "POCKET", 10, idx)

	if len(results) == 0 {
		t.Fatal("no search results for uppercase query")
	}
	if results[0].Kind != "glossary" {
		t.Errorf("first result Kind = %q, want %q (case-insensitive Term match required). Order: %v",
			results[0].Kind, "glossary", articleIDs(results))
	}
}

// articleIDs is a tiny diagnostic helper for the search-boost tests.
func articleIDs(arts []*model.WikiArticle) []string {
	out := make([]string, len(arts))
	for i, a := range arts {
		out[i] = a.ID
	}
	return out
}
