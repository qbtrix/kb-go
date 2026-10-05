// Tests for article, raw-doc, index and cache storage: save/load round trips,
// frontmatter parsing (including "---" inside values, Python-written files,
// legacy articles without the newer fields, glossary fields), RebuildIndex,
// cache hits/misses, and the traversal guard on LoadArticle (issue #23).

package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func TestParseArticleWithFrontmatter(t *testing.T) {
	md := `---
{
  "title": "Test Article",
  "summary": "A test summary",
  "concepts": ["go", "testing"],
  "categories": ["code"],
  "source_docs": ["abc123"],
  "backlinks": [],
  "word_count": 5,
  "compiled_at": "2026-04-07T10:00:00Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Test Content

This is the body.`

	a, err := ParseArticle("test-article", md)
	if err != nil {
		t.Fatalf("parseArticle failed: %v", err)
	}

	if a.ID != "test-article" {
		t.Errorf("ID = %q, want %q", a.ID, "test-article")
	}
	if a.Title != "Test Article" {
		t.Errorf("Title = %q, want %q", a.Title, "Test Article")
	}
	if a.Summary != "A test summary" {
		t.Errorf("Summary = %q", a.Summary)
	}
	if len(a.Concepts) != 2 || a.Concepts[0] != "go" {
		t.Errorf("Concepts = %v", a.Concepts)
	}
	if a.CompiledWith != "claude-haiku-4-5-20251001" {
		t.Errorf("CompiledWith = %q", a.CompiledWith)
	}
	if !strings.Contains(a.Content, "# Test Content") {
		t.Errorf("Content should contain body, got %q", a.Content)
	}
}

func TestParseArticleNoFrontmatter(t *testing.T) {
	md := "# Just plain markdown\n\nNo frontmatter here."
	a, err := ParseArticle("plain", md)
	if err != nil {
		t.Fatalf("parseArticle failed: %v", err)
	}
	if a.Title != "plain" {
		t.Errorf("Title = %q, want 'plain'", a.Title)
	}
	if a.Content != md {
		t.Errorf("Content should be raw text")
	}
	if a.Version != 1 {
		t.Errorf("Version = %d, want 1", a.Version)
	}
}

func TestParseArticleBadFrontmatter(t *testing.T) {
	md := "---\nnot valid json\n---\n\ncontent"
	_, err := ParseArticle("bad", md)
	if err == nil {
		t.Error("expected error for bad frontmatter")
	}
}

func TestSaveLoadArticle(t *testing.T) {
	scope := "test-roundtrip-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	original := &model.WikiArticle{
		ID:           "test-article",
		Title:        "Test Article",
		Summary:      "A test summary",
		Content:      "# Hello\n\nThis is content.",
		Concepts:     []string{"go", "testing"},
		Categories:   []string{"code"},
		SourceDocs:   []string{"raw123"},
		Backlinks:    []string{"other-article"},
		WordCount:    5,
		CompiledAt:   "2026-04-07T10:00:00Z",
		CompiledWith: "test",
		Version:      1,
	}

	err := SaveArticle(scope, original)
	if err != nil {
		t.Fatalf("saveArticle failed: %v", err)
	}

	loaded, err := LoadArticle(scope, "test-article")
	if err != nil {
		t.Fatalf("loadArticle failed: %v", err)
	}

	if loaded.Title != original.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, original.Title)
	}
	if loaded.Summary != original.Summary {
		t.Errorf("Summary mismatch")
	}
	if loaded.Content != original.Content {
		t.Errorf("Content = %q, want %q", loaded.Content, original.Content)
	}
	if len(loaded.Concepts) != 2 {
		t.Errorf("Concepts = %v", loaded.Concepts)
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d", loaded.Version)
	}

	os.RemoveAll(ScopeDir(scope))
}

// A "---" inside a frontmatter string value (a markdown rule or table divider
// in the summary, a title like "A --- B") must not be read as the closing
// delimiter. Before the fix, ParseArticle split on the first "---" anywhere,
// the JSON parse failed, and ListArticles silently dropped the article from
// every index and search.
func TestSaveLoadArticleDashesInFrontmatter(t *testing.T) {
	scope := "test-dashes-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	cases := []*model.WikiArticle{
		{ID: "rule-in-summary", Title: "Rule In Summary", Summary: "# Doc\n\nIntro.\n\n---\n\n## Next", Content: "body one"},
		{ID: "table-in-summary", Title: "Table In Summary", Summary: "| a | b |\n|---|---|\n| 1 | 2 |", Content: "body two"},
		{ID: "dashes-in-title", Title: "Before --- After", Summary: "plain", Content: "body three\n\n---\n\nmore"},
		{ID: "plain", Title: "Plain", Summary: "no dashes", Content: "body four"},
	}
	for _, a := range cases {
		if err := SaveArticle(scope, a); err != nil {
			t.Fatalf("saveArticle(%s): %v", a.ID, err)
		}
	}

	for _, want := range cases {
		got, err := LoadArticle(scope, want.ID)
		if err != nil {
			t.Errorf("loadArticle(%s): %v", want.ID, err)
			continue
		}
		if got.Title != want.Title || got.Summary != want.Summary || got.Content != want.Content {
			t.Errorf("%s round-trip mismatch: title=%q summary=%q content=%q", want.ID, got.Title, got.Summary, got.Content)
		}
	}

	all, err := ListArticles(scope)
	if err != nil {
		t.Fatalf("listArticles: %v", err)
	}
	if len(all) != len(cases) {
		ids := make([]string, 0, len(all))
		for _, a := range all {
			ids = append(ids, a.ID)
		}
		t.Errorf("listArticles returned %d articles %v, want %d", len(all), ids, len(cases))
	}
}

func TestSaveLoadRawDoc(t *testing.T) {
	scope := "test-raw-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	raw := &model.RawDoc{
		ID:          "abc123",
		SourceType:  "file",
		Source:      "test.py",
		Filename:    "test.py",
		ContentType: "text",
		RawText:     "print('hello')",
		WordCount:   1,
		IngestedAt:  "2026-04-07T10:00:00Z",
	}

	err := SaveRawDoc(scope, raw)
	if err != nil {
		t.Fatalf("saveRawDoc failed: %v", err)
	}

	loaded, err := LoadRawDoc(scope, "abc123")
	if err != nil {
		t.Fatalf("loadRawDoc failed: %v", err)
	}
	if loaded.RawText != "print('hello')" {
		t.Errorf("RawText = %q", loaded.RawText)
	}
	if loaded.Source != "test.py" {
		t.Errorf("Source = %q", loaded.Source)
	}
}

func TestRebuildIndex(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Title: "Auth Service", Concepts: []string{"auth", "JWT"}, Categories: []string{"code"}},
		{ID: "a2", Title: "User Service", Concepts: []string{"auth", "users"}, Categories: []string{"code", "api"}},
	}

	idx := RebuildIndex("test", articles)

	if len(idx.Articles) != 2 {
		t.Errorf("Articles count = %d, want 2", len(idx.Articles))
	}

	authConcept := idx.Concepts["auth"]
	if authConcept == nil {
		t.Fatal("auth concept not found")
	}
	if len(authConcept.Articles) != 2 {
		t.Errorf("auth concept has %d articles, want 2", len(authConcept.Articles))
	}

	jwtConcept := idx.Concepts["jwt"]
	if jwtConcept == nil {
		t.Fatal("jwt concept not found")
	}
	if len(jwtConcept.Articles) != 1 {
		t.Errorf("jwt concept has %d articles, want 1", len(jwtConcept.Articles))
	}

	if len(idx.Categories) != 2 {
		t.Errorf("Categories = %v, want [api, code]", idx.Categories)
	}
}

func TestSaveLoadIndex(t *testing.T) {
	scope := "test-idx-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	idx := &model.KnowledgeIndex{
		Scope:      scope,
		Articles:   map[string]any{"a1": map[string]any{"title": "Test"}},
		Concepts:   map[string]*model.Concept{"go": {Name: "Go", Articles: []string{"a1"}}},
		Categories: []string{"code"},
	}

	err := SaveIndex(scope, idx)
	if err != nil {
		t.Fatalf("saveIndex failed: %v", err)
	}

	loaded := LoadIndex(scope)
	if loaded.Scope != scope {
		t.Errorf("Scope = %q", loaded.Scope)
	}
	if len(loaded.Concepts) != 1 {
		t.Errorf("Concepts count = %d", len(loaded.Concepts))
	}
}

func TestCacheRoundTrip(t *testing.T) {
	scope := "test-cache-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	c := &model.Cache{
		Version: 1,
		Files: map[string]model.CacheEntry{
			"src/main.py": {Hash: "abc123", ArticleID: "main-py", CompiledAt: "2026-04-07T10:00:00Z"},
		},
	}

	EnsureDirs(scope)
	err := SaveCache(scope, c)
	if err != nil {
		t.Fatalf("saveCache failed: %v", err)
	}

	loaded := LoadCache(scope)
	if loaded.Version != 1 {
		t.Errorf("Version = %d", loaded.Version)
	}
	entry, ok := loaded.Files["src/main.py"]
	if !ok {
		t.Fatal("cache entry not found")
	}
	if entry.Hash != "abc123" {
		t.Errorf("Hash = %q", entry.Hash)
	}
}

func TestCacheHit(t *testing.T) {
	text := "print('hello world')"
	hash := textutil.ContentHash(text)

	cache := &model.Cache{
		Version: 1,
		Files: map[string]model.CacheEntry{
			"test.py": {Hash: hash, ArticleID: "test-py"},
		},
	}

	entry, ok := cache.Files["test.py"]
	if !ok || entry.Hash != hash {
		t.Error("expected cache hit for same content")
	}
}

func TestCacheMiss(t *testing.T) {
	cache := &model.Cache{
		Version: 1,
		Files: map[string]model.CacheEntry{
			"test.py": {Hash: "old-hash", ArticleID: "test-py"},
		},
	}

	newHash := textutil.ContentHash("modified content")
	entry := cache.Files["test.py"]
	if entry.Hash == newHash {
		t.Error("expected cache miss for changed content")
	}
}

func TestPythonFormatCompatibility(t *testing.T) {
	// This is the exact format that the Python knowledge-base package writes.
	// Go must be able to read it.
	pythonOutput := `---
{
  "title": "GroupService",
  "summary": "Handles group CRUD operations",
  "concepts": ["GroupService", "membership", "Beanie ODM"],
  "categories": ["code"],
  "source_docs": ["abc123def456"],
  "backlinks": ["message_service"],
  "word_count": 450,
  "compiled_at": "2026-04-06T18:00:00+00:00",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 2
}
---

# GroupService

Handles group creation, membership, and settings.

## Classes

### GroupService(BaseService)

Main service for group operations.`

	a, err := ParseArticle("group_service", pythonOutput)
	if err != nil {
		t.Fatalf("Failed to parse Python-format article: %v", err)
	}

	if a.Title != "GroupService" {
		t.Errorf("Title = %q", a.Title)
	}
	if len(a.Concepts) != 3 {
		t.Errorf("Concepts = %v", a.Concepts)
	}
	if a.Version != 2 {
		t.Errorf("Version = %d", a.Version)
	}
	if !strings.Contains(a.Content, "# GroupService") {
		t.Error("Content missing body")
	}
	if a.Backlinks[0] != "message_service" {
		t.Errorf("Backlinks = %v", a.Backlinks)
	}
}

// TestFrontmatterAudienceDepthRoundTrip verifies that Audience, Depth, and
// TargetWords survive a save→load round-trip intact.
func TestFrontmatterAudienceDepthRoundTrip(t *testing.T) {
	scope := "test-terse-rt-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	original := &model.WikiArticle{
		ID:           "terse-article",
		Title:        "Terse Article",
		Summary:      "Short overview",
		Content:      "# Overview\n\nWhat it does.",
		Concepts:     []string{"auth"},
		Categories:   []string{"code"},
		SourceDocs:   []string{"raw001"},
		WordCount:    5,
		CompiledAt:   "2026-04-23T10:00:00Z",
		CompiledWith: "test",
		Version:      1,
		Audience:     "agent",
		Depth:        "overview",
		TargetWords:  150,
	}

	if err := SaveArticle(scope, original); err != nil {
		t.Fatalf("saveArticle failed: %v", err)
	}

	loaded, err := LoadArticle(scope, "terse-article")
	if err != nil {
		t.Fatalf("loadArticle failed: %v", err)
	}

	if loaded.Audience != "agent" {
		t.Errorf("Audience = %q, want %q", loaded.Audience, "agent")
	}
	if loaded.Depth != "overview" {
		t.Errorf("Depth = %q, want %q", loaded.Depth, "overview")
	}
	if loaded.TargetWords != 150 {
		t.Errorf("TargetWords = %d, want 150", loaded.TargetWords)
	}
}

// TestFrontmatterLoadBackwardCompat verifies that old .md files without
// audience/depth/target_words fields load with sensible defaults.
func TestFrontmatterLoadBackwardCompat(t *testing.T) {
	scope := "test-terse-compat-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	// Write a .md file that looks like it was produced before the terse feature.
	oldFmt := `---
{
  "title": "Old Article",
  "summary": "A legacy summary",
  "concepts": ["legacy"],
  "categories": ["code"],
  "source_docs": ["abc123"],
  "backlinks": [],
  "word_count": 100,
  "compiled_at": "2026-01-01T00:00:00Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Old Article

This article predates the terse feature.`

	EnsureDirs(scope)
	wikiDir := filepath.Join(ScopeDir(scope), "wiki")
	os.MkdirAll(wikiDir, 0o755)
	if err := os.WriteFile(filepath.Join(wikiDir, "old-article.md"), []byte(oldFmt), 0o644); err != nil {
		t.Fatalf("failed to write test fixture: %v", err)
	}

	loaded, err := LoadArticle(scope, "old-article")
	if err != nil {
		t.Fatalf("loadArticle failed: %v", err)
	}

	// Defaults: audience=human, depth=deep, target_words=500
	if loaded.Audience != "human" {
		t.Errorf("Audience = %q, want %q (default)", loaded.Audience, "human")
	}
	if loaded.Depth != "deep" {
		t.Errorf("Depth = %q, want %q (default)", loaded.Depth, "deep")
	}
	if loaded.TargetWords != 500 {
		t.Errorf("TargetWords = %d, want 500 (default)", loaded.TargetWords)
	}
}

func TestSourcePathRoundTrip(t *testing.T) {
	scope := "test-src-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	original := &model.WikiArticle{
		ID:         "src-round",
		Title:      "Src Round",
		Content:    "# body",
		SourcePath: "src/auth/login.go",
		SourceDocs: []string{"raw-x"},
		Version:    1,
	}
	if err := SaveArticle(scope, original); err != nil {
		t.Fatalf("saveArticle: %v", err)
	}
	loaded, err := LoadArticle(scope, "src-round")
	if err != nil {
		t.Fatalf("loadArticle: %v", err)
	}
	if loaded.SourcePath != "src/auth/login.go" {
		t.Errorf("SourcePath = %q, want %q", loaded.SourcePath, "src/auth/login.go")
	}
}

func TestSourcePathEmptyOmitsFromJSON(t *testing.T) {
	// Backward compat: legacy articles without SourcePath should parse cleanly
	// and the omitted field should stay out of the serialized frontmatter.
	scope := "test-src-empty-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	a := &model.WikiArticle{ID: "legacy", Title: "Legacy", Content: "body", Version: 1}
	if err := SaveArticle(scope, a); err != nil {
		t.Fatalf("saveArticle: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(ScopeDir(scope), "wiki", "legacy.md"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(data), `"source_path"`) {
		t.Error("empty SourcePath should be omitted from JSON (omitempty)")
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryFrontmatterRoundTrip(t *testing.T) {
	scope := "test-gloss-rt-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	original := &model.WikiArticle{
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

	if err := SaveArticle(scope, original); err != nil {
		t.Fatalf("saveArticle: %v", err)
	}
	loaded, err := LoadArticle(scope, "pocket")
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

// TODO: passes after glossary feature lands
func TestGlossaryParseArticleFromFile(t *testing.T) {
	scope := "test-gloss-parse-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(ScopeDir(scope)) }()

	EnsureDirs(scope)
	wikiDir := filepath.Join(ScopeDir(scope), "wiki")
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

	a, err := LoadArticle(scope, "pocket")
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

func TestLoadArticle_RejectsTraversalID(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "contain-" + filepath.Base(dir)
	EnsureDirs(scope)

	// Prove the escape, not a coincidental not-found. id="../../secret" resolves
	// after filepath.Join's Clean to <base>/secret.md — outside the scope's wiki
	// AND outside the scope dir. Plant a readable article body there so a
	// successful (vulnerable) read returns its contents; containment must reject
	// before the read.
	escapeTarget := filepath.Join(BaseDir(), "secret.md")
	if err := os.WriteFile(escapeTarget, []byte("# Leaked\n\nTOP SECRET"), 0o644); err != nil {
		t.Fatalf("plant escape target: %v", err)
	}
	// Sanity: confirm the planted file is exactly where "../../secret" lands.
	resolved := filepath.Join(ScopeDir(scope), "wiki", "../../secret"+".md")
	if resolved != escapeTarget {
		t.Fatalf("test assumption broke: %q != %q", resolved, escapeTarget)
	}

	bad := []string{
		"../../secret",          // the proven escape target above
		"../../../../etc/hosts", // classic deep traversal
		"../secret",
		"sub/../../secret",
		"a/b/c",
		`..\..\secret`,
		`a\b`,
		"/etc/hosts",
		"..",
	}
	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			a, err := LoadArticle(scope, id)
			if err == nil {
				t.Fatalf("loadArticle(%q) returned no error; traversal not contained (got article %+v)", id, a)
			}
			if a != nil {
				t.Fatalf("loadArticle(%q) returned a non-nil article on a rejected id", id)
			}
			// A vulnerable read would surface the planted body either as a
			// returned article or echoed in the error.
			if strings.Contains(err.Error(), "TOP SECRET") {
				t.Fatalf("loadArticle(%q) leaked file contents in error: %v", id, err)
			}
		})
	}
}

func TestLoadArticle_AllowsLegitimateSlugIDs(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "contain-ok-" + filepath.Base(dir)

	// All ids kb-go actually generates: slugify() output (lowercase, digits,
	// hyphens), contentHash hex, and term slugs. None contain a separator or
	// "..". Each must still resolve after containment.
	ids := []string{
		"my-article",
		"rate-limiter-pattern",
		"a1b2c3d4e5f6a7b8", // contentHash[:16] shape
		"pocket",           // glossary term slug
		"soul-protocol",
		"single",
	}
	for _, id := range ids {
		stubArticle(t, scope, id, "Title "+id, "summary", "# Body\n\ncontent")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			a, err := LoadArticle(scope, id)
			if err != nil {
				t.Fatalf("loadArticle(%q) rejected a legitimate id: %v", id, err)
			}
			if a == nil || a.ID != id {
				t.Fatalf("loadArticle(%q) did not round-trip; got %+v", id, a)
			}
		})
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
