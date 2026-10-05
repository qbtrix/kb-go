// Tests for structural lint (empty KB, missing concepts, broken backlinks) and
// category normalisation: NormalizeCategory, PickCanonical, AffectedCount, and
// ApplyCanonical (rewrites, collapsed duplicates, and the index persisted).

package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func TestLintEmptyKB(t *testing.T) {
	scope := "test-lint-empty-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()
	store.EnsureDirs(scope)

	issues := Structural(scope)
	if len(issues) == 0 {
		t.Error("expected warning for empty KB")
	}
	if issues[0].Type != "gap" {
		t.Errorf("expected 'gap' issue, got %q", issues[0].Type)
	}
}

func TestLintMissingConcepts(t *testing.T) {
	scope := "test-lint-concepts-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	article := &model.WikiArticle{
		ID:       "test",
		Title:    "Test",
		Summary:  "A test",
		Content:  "Some content",
		Concepts: []string{}, // empty
		Version:  1,
	}
	store.SaveArticle(scope, article)

	issues := Structural(scope)
	found := false
	for _, issue := range issues {
		if issue.Type == "gap" && strings.Contains(issue.Message, "no concepts") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing concepts")
	}
}

func TestLintBrokenBacklink(t *testing.T) {
	scope := "test-lint-backlink-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	article := &model.WikiArticle{
		ID:        "test",
		Title:     "Test",
		Summary:   "A test",
		Content:   "Content",
		Concepts:  []string{"test"},
		Backlinks: []string{"nonexistent"},
		Version:   1,
	}
	store.SaveArticle(scope, article)

	issues := Structural(scope)
	found := false
	for _, issue := range issues {
		if issue.Type == "connection" && strings.Contains(issue.Message, "broken backlink") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about broken backlink")
	}
}

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"CLI", "cli"},
		{"CLI tool", "cli tool"},
		{"cli tool", "cli tool"},
		{"  cli  tool  ", "cli tool"},
		{"Database.", "database"},
		{"Storage--", "storage"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := NormalizeCategory(tt.in); got != tt.want {
			t.Errorf("normalizeCategory(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPickCanonicalVariant(t *testing.T) {
	// Count dominates
	got := PickCanonical(map[string]int{"cli": 10, "CLI": 2, "CLI tool": 1})
	if got != "cli" {
		t.Errorf("expected 'cli' (highest count), got %q", got)
	}
	// Count tie → shortest wins
	got = PickCanonical(map[string]int{"database": 3, "Database engine": 3})
	if got != "database" {
		t.Errorf("expected 'database' (shortest on tie), got %q", got)
	}
	// All tied on count + length, all already-normalized → alphabetical
	got = PickCanonical(map[string]int{"zebra": 1, "alpha": 1, "bravo": 1})
	if got != "alpha" {
		t.Errorf("expected 'alpha' (alphabetical fallback), got %q", got)
	}
	// Tied on count + length, one matches its own normalize() → clean form wins
	got = PickCanonical(map[string]int{"Storage": 1, "storage": 1})
	if got != "storage" {
		t.Errorf("expected 'storage' (clean form preferred on tie), got %q", got)
	}
}

func TestApplyCategoryCanonical(t *testing.T) {
	scope := "test-norm-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	articles := []*model.WikiArticle{
		{ID: "a1", Title: "A1", Content: "x", Categories: []string{"CLI", "storage"}, Version: 1},
		{ID: "a2", Title: "A2", Content: "x", Categories: []string{"cli", "Storage"}, Version: 1},
		{ID: "a3", Title: "A3", Content: "x", Categories: []string{"cli tool", "database"}, Version: 1},
		{ID: "a4", Title: "A4", Content: "x", Categories: []string{"cli"}, Version: 1},
	}
	for _, a := range articles {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	// Build clusters manually mirroring runCategoryNormalize's logic so the
	// test exercises ApplyCanonical in isolation.
	all, _ := store.ListArticles(scope)
	clusterMap := map[string]*CategoryCluster{}
	for _, a := range all {
		for _, cat := range a.Categories {
			key := NormalizeCategory(cat)
			c, ok := clusterMap[key]
			if !ok {
				c = &CategoryCluster{Key: key, Variants: map[string]int{}}
				clusterMap[key] = c
			}
			c.Variants[cat]++
			c.Total++
		}
	}
	var noisy []*CategoryCluster
	for _, c := range clusterMap {
		if len(c.Variants) > 1 {
			c.Canonical = PickCanonical(c.Variants)
			noisy = append(noisy, c)
		}
	}

	changed := ApplyCanonical(scope, all, noisy)
	if changed == 0 {
		t.Fatal("expected at least one article rewritten")
	}

	// Verify: after apply, only canonical variants remain from multi-variant clusters.
	// Cluster "cli" has variants {CLI, cli} → canonical "cli" (ASCII sort tie; "cli" is clean form)
	// Cluster "storage" has variants {storage, Storage} → canonical "storage" (clean form preferred on tie)
	// "cli tool" and "database" are singleton clusters — not noisy, unchanged.
	after, _ := store.ListArticles(scope)
	seenVariants := map[string]bool{}
	for _, a := range after {
		for _, cat := range a.Categories {
			seenVariants[cat] = true
		}
	}
	for _, bad := range []string{"CLI", "Storage"} {
		if seenVariants[bad] {
			t.Errorf("%q should have been canonicalized out", bad)
		}
	}
	for _, good := range []string{"cli", "storage", "cli tool", "database"} {
		if !seenVariants[good] {
			t.Errorf("expected %q to remain after normalization", good)
		}
	}
}

func TestApplyCategoryCanonicalDedupesCollapsed(t *testing.T) {
	// Article with ["CLI", "cli"] should collapse to ["cli"] — no dupes.
	scope := "test-norm-dedup-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	a := &model.WikiArticle{
		ID:         "d1",
		Title:      "D1",
		Content:    "x",
		Categories: []string{"CLI", "cli"},
		Version:    1,
	}
	if err := store.SaveArticle(scope, a); err != nil {
		t.Fatalf("save: %v", err)
	}
	all, _ := store.ListArticles(scope)
	noisy := []*CategoryCluster{
		{
			Key:       "cli",
			Variants:  map[string]int{"CLI": 1, "cli": 1},
			Total:     2,
			Canonical: "cli",
		},
	}
	if changed := ApplyCanonical(scope, all, noisy); changed != 1 {
		t.Errorf("expected 1 article changed, got %d", changed)
	}
	loaded, _ := store.LoadArticle(scope, "d1")
	if len(loaded.Categories) != 1 || loaded.Categories[0] != "cli" {
		t.Errorf("categories = %v, want [cli]", loaded.Categories)
	}
}

func TestApplyCategoryPersistsIndex(t *testing.T) {
	// Regression: after --apply rewrites categories, the on-disk BM25 index
	// must reflect the new category set. Earlier version built the index in
	// memory but forgot to save it, so `kb stats` kept showing stale counts.
	scope := "test-norm-idx-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	articles := []*model.WikiArticle{
		{ID: "a1", Title: "A1", Content: "x", Categories: []string{"CLI"}, Version: 1},
		{ID: "a2", Title: "A2", Content: "x", Categories: []string{"cli"}, Version: 1},
	}
	for _, a := range articles {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	// Initial index with both variants
	all, _ := store.ListArticles(scope)
	_ = store.SaveIndex(scope, store.RebuildIndex(scope, all))

	noisy := []*CategoryCluster{{
		Key:       "cli",
		Variants:  map[string]int{"CLI": 1, "cli": 1},
		Canonical: "cli",
	}}
	if changed := ApplyCanonical(scope, all, noisy); changed == 0 {
		t.Fatal("expected apply to rewrite")
	}

	// Load the persisted index from disk and verify it reflects the collapse.
	data, err := os.ReadFile(filepath.Join(store.ScopeDir(scope), "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var persisted model.KnowledgeIndex
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("parse index: %v", err)
	}
	if len(persisted.Categories) != 1 {
		t.Errorf("persisted categories = %v, want [cli] only", persisted.Categories)
	}
	if persisted.Categories[0] != "cli" {
		t.Errorf("persisted category = %q, want 'cli'", persisted.Categories[0])
	}
}

func TestAffectedArticleCount(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Categories: []string{"cli"}},            // already canonical
		{ID: "a2", Categories: []string{"CLI"}},            // needs rewrite
		{ID: "a3", Categories: []string{"storage"}},        // not in clusters
		{ID: "a4", Categories: []string{"cli", "Storage"}}, // one needs rewrite
	}
	clusters := []*CategoryCluster{
		{
			Key:       "cli",
			Variants:  map[string]int{"cli": 10, "CLI": 1},
			Canonical: "cli",
		},
	}
	got := AffectedCount(articles, clusters)
	if got != 1 {
		t.Errorf("affectedArticleCount = %d, want 1", got)
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
