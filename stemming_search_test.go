// stemming_search_test.go — End-to-end BM25 retrieval proofs for stemming
// (feat/bm25-stemming). Created: 2026-07-15.
//
// porter_test.go proves the stemmer in isolation; these prove the actual recall
// fix through bm25Search/bm25SearchWithIndex: a query in one morphological form
// retrieves a document written in another, the glossary boost survives (and now
// also fires on variants), and the over-stem false positive does NOT happen.

package main

import "testing"

// resultHasID reports whether an article with the given ID is in the results.
func resultHasID(results []*WikiArticle, id string) bool {
	for _, a := range results {
		if a.ID == id {
			return true
		}
	}
	return false
}

// TestStemmingRetrieval_OpenOpens is the exact live-smoke case (T5): a doc that
// only ever says "opens"/"opening" — never the bare word "open" — must be
// retrieved by the query "open". Before stemming, BM25 was lexical and returned
// nothing here.
func TestStemmingRetrieval_OpenOpens(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "hours", Title: "Store Hours", Content: "The shop opens at 8am and closes at 6pm.", Version: 1},
		{ID: "unrelated", Title: "Payments", Content: "We accept cards and cash for all purchases.", Version: 1},
	}

	for _, q := range []string{"open", "opening"} {
		results := bm25Search(articles, q, 5)
		if !resultHasID(results, "hours") {
			t.Errorf("query %q did not retrieve the 'opens at 8am' doc; results=%v", q, articleIDs(results))
		}
	}
}

// TestStemmingRetrieval_MorphologicalPairs covers two more families end-to-end.
func TestStemmingRetrieval_MorphologicalPairs(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "loc", Title: "Where We Are", Content: "The clinic is located behind the central library.", Version: 1},
		{ID: "menu", Title: "Kitchen", Content: "The kitchen serves lunch and dinner every day.", Version: 1},
		{ID: "noise", Title: "About", Content: "A friendly neighborhood establishment since 1990.", Version: 1},
	}

	// "located" doc retrieved by the noun query "location".
	if r := bm25Search(articles, "location", 5); !resultHasID(r, "loc") {
		t.Errorf("query %q did not retrieve the 'located' doc; results=%v", "location", articleIDs(r))
	}
	// "serves" doc retrieved by the gerund query "serving".
	if r := bm25Search(articles, "serving", 5); !resultHasID(r, "menu") {
		t.Errorf("query %q did not retrieve the 'serves' doc; results=%v", "serving", articleIDs(r))
	}
}

// TestStemmingRetrieval_NoOverStem is the over-stem guard at the retrieval
// level: a query "open" must NOT surface a document that is only about
// "operators" (open->"open", operator->"oper", so they must not match).
func TestStemmingRetrieval_NoOverStem(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "ops", Title: "Operators", Content: "Mobile network operators route calls through regional operator hubs.", Version: 1},
	}
	if r := bm25Search(articles, "open", 5); resultHasID(r, "ops") {
		t.Errorf("over-stem: query %q wrongly retrieved the operator-only doc; results=%v", "open", articleIDs(r))
	}
}

// TestStemmingRetrieval_GlossaryExactStillFirst is the regression guard: an
// exact term query still ranks the glossary article first (the boost survives
// stemming both sides of the comparison).
func TestStemmingRetrieval_GlossaryExactStillFirst(t *testing.T) {
	articles := makeSearchCorpus()
	idx := buildSearchIndex(articles)
	results := bm25SearchWithIndex(articles, "pocket", 10, idx)
	if len(results) == 0 {
		t.Fatal("no results for exact term query 'pocket'")
	}
	if results[0].Kind != "glossary" {
		t.Errorf("first result Kind = %q, want glossary (exact-term boost must survive stemming); order=%v",
			results[0].Kind, articleIDs(results))
	}
}

// TestStemmingRetrieval_GlossaryVariantBoost proves the added benefit AND
// guards the glossary-block change specifically. The glossary Term is plural
// ("Connectors") but the query is singular ("connector"). Only because the
// boost stems BOTH sides ("connectors"->"connector") does the glossary article
// win the 10x boost and outrank a module doc that mentions the word far more
// often. Without stemming the Term side, the raw "connectors" != "connector"
// query, the boost would not fire, and the mention-heavy module doc would rank
// first — so this test fails if the glossary block regresses.
func TestStemmingRetrieval_GlossaryVariantBoost(t *testing.T) {
	articles := []*WikiArticle{
		{
			ID:      "connector-guide",
			Title:   "Connector Guide",
			Content: "connector connector connector connector connector connector setup",
			Version: 1,
		},
		{
			ID:      "connectors-def",
			Title:   "Connectors",
			Content: "Connectors bind external services into a pocket.",
			Kind:    "glossary",
			Term:    "Connectors", // plural, non-stem-stable: stems to "connector"
			Version: 1,
		},
	}
	idx := buildSearchIndex(articles)
	results := bm25SearchWithIndex(articles, "connector", 10, idx)
	if len(results) == 0 {
		t.Fatal("no results for query 'connector'")
	}
	if results[0].Kind != "glossary" {
		t.Errorf("first result Kind = %q, want glossary (stemmed-Term boost on plural term); order=%v",
			results[0].Kind, articleIDs(results))
	}
}
