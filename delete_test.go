// delete_test.go — Tests for the `kb delete` subcommand (cmdDelete).
//
// Covers the acceptance slice: deleting an article removes it from the on-disk
// raw/wiki files, the knowledge index (Articles map + Concepts graph +
// Categories set), and the caches (BM25 search index, compile-hash cache,
// vector index). Also pins the two edge behaviours the purge-on-hide caller
// depends on: idempotent no-op for an id that is already gone, and traversal
// refusal for a path-like id (mirrors path_containment_test.go).
//
// These exercise cmdDelete through its real disk side effects rather than
// mocking the storage layer, so a live regression (stale cache surviving,
// dangling concept ref) fails the test instead of going green.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

// seedArticle writes an article + its hash-keyed raw doc (mirroring what ingest
// leaves on disk, where the raw doc id differs from the article slug) with the
// given concepts + categories. The raw doc id is "<id>-raw" so the test can
// prove cmdDelete removes the SourceDocs-referenced raw doc, not just
// raw/<id>.json.
func seedArticle(t *testing.T, scope, id, title string, concepts, categories []string) {
	t.Helper()
	rawID := id + "-raw"
	raw := &RawDoc{
		ID:          rawID,
		SourceType:  "file",
		Source:      "src/" + id + ".go",
		ContentType: "text",
		RawText:     "raw text for " + id,
		WordCount:   3,
		IngestedAt:  "2026-07-03T00:00:00Z",
	}
	if err := saveRawDoc(scope, raw); err != nil {
		t.Fatalf("saveRawDoc %s: %v", rawID, err)
	}
	a := &WikiArticle{
		ID:           id,
		Title:        title,
		Summary:      "summary for " + id,
		Content:      "# " + title + "\n\nbody text for " + id,
		Concepts:     concepts,
		Categories:   categories,
		SourceDocs:   []string{rawID},
		Backlinks:    []string{},
		WordCount:    3,
		CompiledAt:   "2026-07-03T00:00:00Z",
		CompiledWith: "test",
		Version:      1,
	}
	if err := saveArticle(scope, a); err != nil {
		t.Fatalf("saveArticle %s: %v", id, err)
	}
}

func flushIndexes(t *testing.T, scope string) {
	t.Helper()
	all, _ := listArticles(scope)
	if err := saveSearchIndex(scope, buildSearchIndex(all)); err != nil {
		t.Fatalf("saveSearchIndex: %v", err)
	}
	if err := saveIndex(scope, rebuildIndex(scope, all)); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}
}

func TestCmdDelete_RemovesArticleEverywhere(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "del-" + filepath.Base(dir)
	ensureDirs(scope)

	// Two articles that share concept "auth" so we can prove the concept
	// survives (still referenced by the survivor) while a solo concept is
	// dropped. "target" also owns a solo concept "sessions" and a solo
	// category "SoloCat".
	seedArticle(t, scope, "target", "Target Article",
		[]string{"auth", "sessions"}, []string{"Backend", "SoloCat"})
	seedArticle(t, scope, "keeper", "Keeper Article",
		[]string{"auth"}, []string{"Backend"})
	flushIndexes(t, scope)

	// Attach a vector to the target so we can prove it's removed.
	vidx := NewVectorIndex()
	vidx.Add("target", []float32{0.1, 0.2})
	vidx.Add("keeper", []float32{0.3, 0.4})
	if err := saveVectorIndex(scope, vidx); err != nil {
		t.Fatalf("saveVectorIndex: %v", err)
	}

	// Plant a hash-cache entry mapping some source path -> target.
	cache := loadCache(scope)
	cache.Files["src/target.go"] = CacheEntry{Hash: "abc", ArticleID: "target", CompiledAt: "x"}
	cache.Files["src/keeper.go"] = CacheEntry{Hash: "def", ArticleID: "keeper", CompiledAt: "x"}
	if err := saveCache(scope, cache); err != nil {
		t.Fatalf("saveCache: %v", err)
	}

	// Sanity: before deletion search finds the target.
	before, _ := listArticles(scope)
	si := loadSearchIndex(scope)
	hits := bm25SearchWithIndex(before, "Target", 10, si)
	found := false
	for _, h := range hits {
		if h.ID == "target" {
			found = true
		}
	}
	if !found {
		t.Fatalf("precondition: search did not find target before delete")
	}

	// --- delete ---
	cmdDelete([]string{"target", "--scope", scope})

	// Files gone — wiki article AND its SourceDocs-referenced raw doc.
	if fileExists(filepath.Join(scopeDir(scope), "wiki", "target.md")) {
		t.Errorf("wiki/target.md still present after delete")
	}
	if fileExists(filepath.Join(scopeDir(scope), "raw", "target-raw.json")) {
		t.Errorf("raw/target-raw.json (SourceDocs raw doc) still present after delete")
	}
	// Keeper untouched — wiki AND raw doc survive.
	if !fileExists(filepath.Join(scopeDir(scope), "wiki", "keeper.md")) {
		t.Errorf("wiki/keeper.md was removed but should survive")
	}
	if !fileExists(filepath.Join(scopeDir(scope), "raw", "keeper-raw.json")) {
		t.Errorf("raw/keeper-raw.json was removed but should survive")
	}

	// Index: target gone, keeper stays.
	idx := loadIndex(scope)
	if _, ok := idx.Articles["target"]; ok {
		t.Errorf("index still lists target after delete")
	}
	if _, ok := idx.Articles["keeper"]; !ok {
		t.Errorf("index dropped keeper by mistake")
	}

	// Concept graph: "sessions" (solo) dropped, "auth" (shared) stays but no
	// longer references target.
	if _, ok := idx.Concepts["sessions"]; ok {
		t.Errorf("solo concept 'sessions' should be dropped after its only article was deleted")
	}
	authC, ok := idx.Concepts["auth"]
	if !ok {
		t.Fatalf("shared concept 'auth' was dropped but keeper still uses it")
	}
	for _, aid := range authC.Articles {
		if aid == "target" {
			t.Errorf("concept 'auth' still references deleted article target: %v", authC.Articles)
		}
	}

	// Categories: solo category dropped, shared category survives.
	if contains(idx.Categories, "SoloCat") {
		t.Errorf("solo category 'SoloCat' should be dropped: %v", idx.Categories)
	}
	if !contains(idx.Categories, "Backend") {
		t.Errorf("shared category 'Backend' should survive: %v", idx.Categories)
	}

	// Search cache invalidated (file removed; rebuilds on next search).
	if fileExists(filepath.Join(scopeDir(scope), "cache", "search_index.json")) {
		t.Errorf("search_index.json should be removed to force a clean rebuild")
	}

	// Search no longer returns the target (fresh load from disk).
	after, _ := listArticles(scope)
	hits2 := bm25SearchWithIndex(after, "Target", 10, loadSearchIndex(scope))
	for _, h := range hits2 {
		if h.ID == "target" {
			t.Errorf("search still returns deleted target")
		}
	}

	// Hash cache: target entry dropped, keeper entry retained.
	cache2 := loadCache(scope)
	for _, e := range cache2.Files {
		if e.ArticleID == "target" {
			t.Errorf("hash cache still holds an entry for deleted target")
		}
	}
	if _, ok := cache2.Files["src/keeper.go"]; !ok {
		t.Errorf("hash cache dropped keeper's entry by mistake")
	}

	// Vector index: target removed, keeper retained.
	vidx2, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		t.Fatalf("load vector index: %v", err)
	}
	for _, e := range vidx2.Entries {
		if e.ID == "target" {
			t.Errorf("vector index still holds deleted target")
		}
	}
	keeperVec := false
	for _, e := range vidx2.Entries {
		if e.ID == "keeper" {
			keeperVec = true
		}
	}
	if !keeperVec {
		t.Errorf("vector index dropped keeper's vector by mistake")
	}
}

func TestCmdDelete_NonExistentIsNoOp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "del-noop-" + filepath.Base(dir)
	ensureDirs(scope)

	// No panic, no error, no exit — deleting an id that was never there is a
	// clean no-op. (If cmdDelete called fatal() this test would exit non-zero.)
	cmdDelete([]string{"ghost", "--scope", scope})

	idx := loadIndex(scope)
	if _, ok := idx.Articles["ghost"]; ok {
		t.Errorf("ghost id somehow appeared in index")
	}
}

func TestCmdDelete_RefusesTraversalID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "del-traverse-" + filepath.Base(dir)
	ensureDirs(scope)

	// Plant a file where "../../secret" would land after filepath.Join Clean.
	escapeTarget := filepath.Join(basePath(), "secret.md")
	if err := os.WriteFile(escapeTarget, []byte("# Leaked\n\nTOP SECRET"), 0o644); err != nil {
		t.Fatalf("plant escape target: %v", err)
	}
	resolved := filepath.Join(scopeDir(scope), "wiki", "../../secret"+".md")
	if resolved != escapeTarget {
		t.Fatalf("test assumption broke: %q != %q", resolved, escapeTarget)
	}

	// cmdDelete validates via containedID before touching any path. We can't
	// call cmdDelete directly here because it calls fatal()/os.Exit on a bad
	// id, which would abort the test binary — so assert the guard it relies on
	// rejects every traversal shape, and that the planted file is untouched.
	bad := []string{
		"../../secret",
		"../../../../etc/passwd",
		"../secret",
		"sub/../../secret",
		"a/b/c",
		`..\..\secret`,
		`a\b`,
		"/etc/passwd",
		"..",
		"",
	}
	for _, id := range bad {
		if err := containedID(id); err == nil {
			t.Errorf("containedID(%q) accepted a traversal/invalid id; cmdDelete would join it into a path", id)
		}
	}

	// The planted escape target must still exist — no delete path ran against it.
	if !fileExists(escapeTarget) {
		t.Errorf("planted escape target was removed; traversal not contained")
	}
}
