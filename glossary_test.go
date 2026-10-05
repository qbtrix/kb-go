// glossary_test.go — Tests for the glossary scope feature (issue #15): the
// WikiArticle glossary fields (Kind/Term/Aliases/Category/Related), the
// isGlossarySource path helper, parseGlossarySource, exact-term + alias search
// boosting, and the list/show/validate functions behind `kb glossary`. The
// binary-level glossary build check lives in e2e_test.go.

package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// --- Helpers (local to this file, no collision with kb_test.go) ---

// seedGlossaryArticle writes a glossary WikiArticle to scope and fails the
// test on error. Returns the article pointer for further inspection.
func seedGlossaryArticle(t *testing.T, scope string, a *model.WikiArticle) *model.WikiArticle {
	t.Helper()
	if err := store.SaveArticle(scope, a); err != nil {
		t.Fatalf("seedGlossaryArticle saveArticle(%q): %v", a.ID, err)
	}
	return a
}

// --- 3. isGlossarySource path classifier ----------------------------------------

// TODO: passes after glossary feature lands
func TestIsGlossarySource(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"glossary/pocket.md", true},
		{"docs/glossary/soul.md", true},
		{"docs/wiki/glossary/ripple.md", true},
		{"src/pocket.go", false},
		{"glossary.md", false},          // not inside a glossary/ dir
		{"glossaries/pocket.md", false}, // plural — distinct dirname
		{"", false},
	}
	for _, tc := range cases {
		got := isGlossarySource(tc.in)
		if got != tc.want {
			t.Errorf("isGlossarySource(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// --- 5-7. Search: exact term + alias + case-insensitive boosting ----------------

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
	idx := buildSearchIndex(articles)
	results := bm25SearchWithIndex(articles, "pocket", 10, idx)

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
	idx := buildSearchIndex(articles)
	results := bm25SearchWithIndex(articles, "pkt", 10, idx)

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
	idx := buildSearchIndex(articles)
	results := bm25SearchWithIndex(articles, "POCKET", 10, idx)

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

// --- 8-9. glossaryList -----------------------------------------------------------

// TODO: passes after glossary feature lands
func TestGlossaryListEmpty(t *testing.T) {
	scope := "test-gloss-list-empty-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	var buf bytes.Buffer
	if err := glossaryList(scope, &buf); err != nil {
		t.Errorf("glossaryList on empty scope returned err: %v", err)
	}
	// Don't lock the exact wording yet; assert it ran cleanly.
	_ = buf.String()
}

// TODO: passes after glossary feature lands
func TestGlossaryListMultiple(t *testing.T) {
	scope := "test-gloss-list-multi-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	// 3 glossary articles + 1 module article (Kind="" — default).
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "A Pocket is a container.",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "soul", Title: "Soul", Content: "A Soul is a persistent identity.",
		Kind: "glossary", Term: "Soul", Aliases: []string{"spirit"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "fabric", Title: "Fabric", Content: "Fabric is the connective layer.",
		Kind: "glossary", Term: "Fabric", Version: 1,
	})
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "router-module", Title: "RouterModule", Content: "Routes things.", Version: 1,
	})

	var buf bytes.Buffer
	if err := glossaryList(scope, &buf); err != nil {
		t.Fatalf("glossaryList: %v", err)
	}
	out := buf.String()

	for _, term := range []string{"Pocket", "Soul", "Fabric"} {
		if !strings.Contains(out, term) {
			t.Errorf("output missing term %q. Got:\n%s", term, out)
		}
	}
	if strings.Contains(out, "RouterModule") {
		t.Errorf("output should NOT include module-article title. Got:\n%s", out)
	}
	// Aliases should appear alongside their term.
	if !strings.Contains(out, "pkt") {
		t.Errorf("output missing alias 'pkt' alongside Pocket. Got:\n%s", out)
	}
	if !strings.Contains(out, "spirit") {
		t.Errorf("output missing alias 'spirit' alongside Soul. Got:\n%s", out)
	}
}

// --- 10-12. glossaryShow ---------------------------------------------------------

// TODO: passes after glossary feature lands
func TestGlossaryShowByTerm(t *testing.T) {
	scope := "test-gloss-show-term-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "A Pocket is a workspace container.",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt", "pocket"}, Version: 1,
	})

	// Three case variants — all must resolve.
	for _, q := range []string{"Pocket", "pocket", "POCKET"} {
		var buf bytes.Buffer
		if err := glossaryShow(scope, q, &buf); err != nil {
			t.Errorf("glossaryShow(%q): %v", q, err)
			continue
		}
		if !strings.Contains(buf.String(), "workspace container") {
			t.Errorf("glossaryShow(%q) output missing content. Got:\n%s", q, buf.String())
		}
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryShowByAlias(t *testing.T) {
	scope := "test-gloss-show-alias-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "A Pocket is a workspace container.",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt", "pocket"}, Version: 1,
	})

	var buf bytes.Buffer
	if err := glossaryShow(scope, "pkt", &buf); err != nil {
		t.Fatalf("glossaryShow(pkt): %v", err)
	}
	if !strings.Contains(buf.String(), "workspace container") {
		t.Errorf("alias lookup missing content. Got:\n%s", buf.String())
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryShowMissing(t *testing.T) {
	scope := "test-gloss-show-miss-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	// Empty scope: ensure scope dir exists so the call exercises the lookup
	// path, not the missing-scope path.
	store.EnsureDirs(scope)

	var buf bytes.Buffer
	err := glossaryShow(scope, "Nonexistent", &buf)
	if err == nil {
		t.Fatal("glossaryShow on missing term: expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Nonexistent") {
		t.Errorf("error message should mention the queried term (case-preserved). Got: %v", err)
	}
}

// --- 13-17. glossaryValidate ----------------------------------------------------

// TODO: passes after glossary feature lands
func TestGlossaryValidateClean(t *testing.T) {
	scope := "test-gloss-val-clean-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "Pocket body.",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "soul", Title: "Soul", Content: "Soul body.",
		Kind: "glossary", Term: "Soul", Aliases: []string{"spirit"}, Related: []string{"Pocket"}, Version: 1,
	})

	issues, err := glossaryValidate(scope)
	if err != nil {
		t.Fatalf("glossaryValidate err = %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("clean scope returned issues: %v", issues)
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryValidateDuplicateTerm(t *testing.T) {
	scope := "test-gloss-val-duptm-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket-a", Title: "Pocket A", Content: "first",
		Kind: "glossary", Term: "Pocket", Version: 1,
	})
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket-b", Title: "Pocket B", Content: "second",
		Kind: "glossary", Term: "Pocket", Version: 1,
	})

	issues, _ := glossaryValidate(scope)
	if len(issues) == 0 {
		t.Fatal("expected at least one issue for duplicate Term, got none")
	}
	if !containsIssue(issues, "duplicate") {
		t.Errorf("issues should mention 'duplicate' (case-insensitive). Got: %v", issues)
	}
	if !containsIssue(issues, "Pocket") {
		t.Errorf("issues should mention the colliding term 'Pocket'. Got: %v", issues)
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryValidateDuplicateAlias(t *testing.T) {
	scope := "test-gloss-val-dupal-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "p",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "packet", Title: "Packet", Content: "k",
		Kind: "glossary", Term: "Packet", Aliases: []string{"pkt"}, Version: 1,
	})

	issues, _ := glossaryValidate(scope)
	if len(issues) == 0 {
		t.Fatal("expected at least one issue for duplicate alias")
	}
	if !containsIssue(issues, "alias") {
		t.Errorf("issues should mention 'alias'. Got: %v", issues)
	}
	if !containsIssue(issues, "pkt") {
		t.Errorf("issues should name the duplicated alias 'pkt'. Got: %v", issues)
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryValidateAliasTermCollision(t *testing.T) {
	scope := "test-gloss-val-coll-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "p",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	// Pkt's Term collides with Pocket's alias.
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pkt", Title: "Pkt", Content: "k",
		Kind: "glossary", Term: "Pkt", Version: 1,
	})

	issues, _ := glossaryValidate(scope)
	if len(issues) == 0 {
		t.Fatal("expected at least one issue for alias↔term collision")
	}
	if !containsIssue(issues, "pkt") && !containsIssue(issues, "Pkt") {
		t.Errorf("issues should name the colliding identifier 'pkt' / 'Pkt'. Got: %v", issues)
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryValidateDanglingRelated(t *testing.T) {
	scope := "test-gloss-val-dangl-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	// Pocket points at "Phantom" via Related, but Phantom doesn't exist
	// in the scope (no glossary entry, no alias).
	seedGlossaryArticle(t, scope, &model.WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "p",
		Kind: "glossary", Term: "Pocket", Related: []string{"Phantom"}, Version: 1,
	})

	issues, _ := glossaryValidate(scope)
	if len(issues) == 0 {
		t.Fatal("expected at least one issue for dangling Related reference")
	}
	if !containsIssue(issues, "Phantom") {
		t.Errorf("issues should name the dangling reference 'Phantom'. Got: %v", issues)
	}
	if !containsIssue(issues, "related") && !containsIssue(issues, "reference") {
		t.Errorf("issues should classify as 'related' or 'reference'. Got: %v", issues)
	}
}

// containsIssue is a case-insensitive substring search across a slice of
// validate issues. Returns true if any issue contains the needle.
func containsIssue(issues []string, needle string) bool {
	needle = strings.ToLower(needle)
	for _, s := range issues {
		if strings.Contains(strings.ToLower(s), needle) {
			return true
		}
	}
	return false
}

// Compile-time anchor: prove glossaryList/Show signatures take an io.Writer.
// If the implementer changes the signature, this assignment fails to compile
// and the contract conversation surfaces in code review rather than a buried
// runtime mismatch.
var (
	_ func(string, io.Writer) error         = glossaryList
	_ func(string, string, io.Writer) error = glossaryShow
	_ func(string) ([]string, error)        = glossaryValidate
	_ func(string) bool                     = isGlossarySource
)
