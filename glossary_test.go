// glossary_test.go — Failing test suite for the glossary scope feature (issue #15).
// Created: 2026-05-23
// Locks the API contract for kb-go's domain glossary support: new WikiArticle
// fields (Kind/Term/Aliases/Category/Related), the isGlossarySource path helper,
// the build-skip-LLM passthrough for glossary sources, exact-term + alias
// search boosting, and three new sub-commands (list/show/validate) under
// cmdGlossary. All tests in this file are EXPECTED TO FAIL until the
// implementer wires up the feature in a follow-up commit.
package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// --- Helpers (local to this file, no collision with kb_test.go) ---

// seedGlossaryArticle writes a glossary WikiArticle to scope and fails the
// test on error. Returns the article pointer for further inspection.
func seedGlossaryArticle(t *testing.T, scope string, a *WikiArticle) *WikiArticle {
	t.Helper()
	if err := saveArticle(scope, a); err != nil {
		t.Fatalf("seedGlossaryArticle saveArticle(%q): %v", a.ID, err)
	}
	return a
}

// --- 1. Frontmatter round-trip --------------------------------------------------

// TODO: passes after glossary feature lands
func TestGlossaryFrontmatterRoundTrip(t *testing.T) {
	scope := "test-gloss-rt-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	original := &WikiArticle{
		ID:       "pocket",
		Title:    "Pocket",
		Content:  "A Pocket is a workspace container that holds agents.",
		Kind:     "glossary",
		Term:     "Pocket",
		Aliases:  []string{"pkt", "pocket"},
		Category: "workspace-primitives",
		Related:  []string{"Soul", "Fabric"},
		Version:  1,
	}

	if err := saveArticle(scope, original); err != nil {
		t.Fatalf("saveArticle: %v", err)
	}
	loaded, err := loadArticle(scope, "pocket")
	if err != nil {
		t.Fatalf("loadArticle: %v", err)
	}

	if loaded.Kind != "glossary" {
		t.Errorf("Kind = %q, want %q", loaded.Kind, "glossary")
	}
	if loaded.Term != "Pocket" {
		t.Errorf("Term = %q, want %q", loaded.Term, "Pocket")
	}
	if len(loaded.Aliases) != 2 || loaded.Aliases[0] != "pkt" || loaded.Aliases[1] != "pocket" {
		t.Errorf("Aliases = %v, want [pkt pocket]", loaded.Aliases)
	}
	if loaded.Category != "workspace-primitives" {
		t.Errorf("Category = %q, want %q", loaded.Category, "workspace-primitives")
	}
	if len(loaded.Related) != 2 || loaded.Related[0] != "Soul" || loaded.Related[1] != "Fabric" {
		t.Errorf("Related = %v, want [Soul Fabric]", loaded.Related)
	}
	if !strings.Contains(loaded.Content, "workspace container") {
		t.Errorf("Content lost in round-trip: %q", loaded.Content)
	}
}

// --- 2. Parse from a .md file ---------------------------------------------------

// TODO: passes after glossary feature lands
func TestGlossaryParseArticleFromFile(t *testing.T) {
	scope := "test-gloss-parse-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	ensureDirs(scope)
	wikiDir := filepath.Join(scopeDir(scope), "wiki")
	if err := os.MkdirAll(wikiDir, 0o755); err != nil {
		t.Fatalf("mkdir wiki: %v", err)
	}

	md := `---
{
  "id": "pocket",
  "title": "Pocket",
  "kind": "glossary",
  "term": "Pocket",
  "aliases": ["pkt", "pocket"],
  "category": "workspace-primitives",
  "related": ["Soul", "Fabric"],
  "concepts": [],
  "categories": [],
  "word_count": 12
}
---

A Pocket is a workspace container that holds agents, data, tools, connectors.`

	if err := os.WriteFile(filepath.Join(wikiDir, "pocket.md"), []byte(md), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	a, err := loadArticle(scope, "pocket")
	if err != nil {
		t.Fatalf("loadArticle: %v", err)
	}
	if a.Kind != "glossary" {
		t.Errorf("Kind = %q, want %q", a.Kind, "glossary")
	}
	if a.Term != "Pocket" {
		t.Errorf("Term = %q, want %q", a.Term, "Pocket")
	}
	if len(a.Aliases) != 2 {
		t.Errorf("Aliases = %v, want 2 entries", a.Aliases)
	}
	if a.Category != "workspace-primitives" {
		t.Errorf("Category = %q, want %q", a.Category, "workspace-primitives")
	}
	if len(a.Related) != 2 || a.Related[0] != "Soul" {
		t.Errorf("Related = %v, want [Soul Fabric]", a.Related)
	}
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
		{"glossary.md", false},        // not inside a glossary/ dir
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

// --- 4. Build preserves glossary body verbatim (no LLM rewrite) -----------------

// TODO: passes after glossary feature lands
func TestGlossaryBuildPreservesBodyVerbatim(t *testing.T) {
	// Use the binary subprocess pattern (matches TestNormalizeCategoriesCLI* in
	// kb_test.go). With ANTHROPIC_API_KEY explicitly cleared, the LLM call path
	// would fall back to copying raw text — so the fallback alone can't satisfy
	// this test. The implementer MUST add a glossary-specific branch in cmdBuild
	// that parses the frontmatter and populates Kind/Term/Aliases/Category/Related
	// from the source file. Only then does this test pass.
	srcRoot := t.TempDir()
	glossaryDir := filepath.Join(srcRoot, "glossary")
	if err := os.MkdirAll(glossaryDir, 0o755); err != nil {
		t.Fatalf("mkdir glossary: %v", err)
	}

	const marker = "VERBATIM_BODY_MARKER_42"
	src := `---
{
  "id": "pocket",
  "title": "Pocket",
  "kind": "glossary",
  "term": "Pocket",
  "aliases": ["pkt", "pocket"],
  "category": "workspace-primitives",
  "related": ["Soul", "Fabric"],
  "concepts": [],
  "categories": [],
  "word_count": 12
}
---

A Pocket is a workspace container. ` + marker + ` lives in this body.`

	if err := os.WriteFile(filepath.Join(glossaryDir, "pocket.md"), []byte(src), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	scope := "test-gloss-build-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	binary := buildTestBinary(t)
	cmd := exec.Command(binary, "build", srcRoot, "--scope", scope, "--pattern", "*.md")
	// Deliberately clear ANTHROPIC_API_KEY so any code path that reaches the
	// LLM compile step would fail or fall back. The glossary branch must
	// bypass the LLM entirely.
	cmd.Env = append(os.Environ(), "ANTHROPIC_API_KEY=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kb build failed: %v\noutput: %s", err, out)
	}

	articles, err := listArticles(scope)
	if err != nil {
		t.Fatalf("listArticles: %v", err)
	}
	if len(articles) == 0 {
		t.Fatalf("no articles produced by build; stdout: %s", out)
	}

	var glossaryArt *WikiArticle
	for _, a := range articles {
		if a.Kind == "glossary" {
			glossaryArt = a
			break
		}
	}
	if glossaryArt == nil {
		t.Fatalf("no article with Kind=\"glossary\" produced. articles: %+v", articles)
	}
	if glossaryArt.Term != "Pocket" {
		t.Errorf("Term = %q, want %q", glossaryArt.Term, "Pocket")
	}
	if !strings.Contains(glossaryArt.Content, marker) {
		t.Errorf("Content missing verbatim marker %q. Got: %q", marker, glossaryArt.Content)
	}
}

// --- 5-7. Search: exact term + alias + case-insensitive boosting ----------------

// makeSearchCorpus returns the two-article corpus used by the search boost
// tests: a module article that mentions "pocket" multiple times in its body
// vs a glossary article whose Term == "Pocket" and Aliases include "pkt".
// Without the glossary boost the module article wins (more occurrences).
func makeSearchCorpus() []*WikiArticle {
	return []*WikiArticle{
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
func articleIDs(arts []*WikiArticle) []string {
	out := make([]string, len(arts))
	for i, a := range arts {
		out[i] = a.ID
	}
	return out
}

// --- 8-9. glossaryList -----------------------------------------------------------

// TODO: passes after glossary feature lands
func TestGlossaryListEmpty(t *testing.T) {
	scope := "test-gloss-list-empty-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	var buf bytes.Buffer
	if err := glossaryList(scope, &buf); err != nil {
		t.Errorf("glossaryList on empty scope returned err: %v", err)
	}
	// Don't lock the exact wording yet; assert it ran cleanly.
	_ = buf.String()
}

// TODO: passes after glossary feature lands
func TestGlossaryListMultiple(t *testing.T) {
	scope := "test-gloss-list-multi-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	// 3 glossary articles + 1 module article (Kind="" — default).
	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "A Pocket is a container.",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "soul", Title: "Soul", Content: "A Soul is a persistent identity.",
		Kind: "glossary", Term: "Soul", Aliases: []string{"spirit"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "fabric", Title: "Fabric", Content: "Fabric is the connective layer.",
		Kind: "glossary", Term: "Fabric", Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-show-term-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-show-alias-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-show-miss-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	// Empty scope: ensure scope dir exists so the call exercises the lookup
	// path, not the missing-scope path.
	ensureDirs(scope)

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
	scope := "test-gloss-val-clean-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "Pocket body.",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-val-duptm-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "pocket-a", Title: "Pocket A", Content: "first",
		Kind: "glossary", Term: "Pocket", Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-val-dupal-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "p",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-val-coll-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "p",
		Kind: "glossary", Term: "Pocket", Aliases: []string{"pkt"}, Version: 1,
	})
	// Pkt's Term collides with Pocket's alias.
	seedGlossaryArticle(t, scope, &WikiArticle{
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
	scope := "test-gloss-val-dangl-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	// Pocket points at "Phantom" via Related, but Phantom doesn't exist
	// in the scope (no glossary entry, no alias).
	seedGlossaryArticle(t, scope, &WikiArticle{
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
	_ func(string, io.Writer) error             = glossaryList
	_ func(string, string, io.Writer) error     = glossaryShow
	_ func(string) ([]string, error)            = glossaryValidate
	_ func(string) bool                         = isGlossarySource
)
