// search_index_test.go — Tests for the v2 inverted search index.
//
// Pins: (1) the fast postings path returns the same ranking as the on-the-fly
// slow path across plain, title-boosted, concept-boosted, and glossary-boosted
// queries; (2) save/load round-trips the v2 format; (3) an old-format (v1
// token dump) search_index.json is ignored on load, so search silently uses
// the slow path instead of mis-scoring; (4) a stale index (doc set changed
// without a rebuild) is not trusted for scoring; (5) a full-scope search
// self-heals a missing/old-format index — the file reappears as v2 and
// matches the scope — while tag-filtered searches and empty scopes never
// write one.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func searchTestCorpus() []*WikiArticle {
	return []*WikiArticle{
		{
			ID: "auth-middleware", Title: "Auth Middleware",
			Summary:  "Token validation for incoming requests.",
			Content:  "The auth middleware validates bearer tokens on every request. Sessions expire after an hour.",
			Concepts: []string{"auth", "middleware"}, Categories: []string{"Backend"},
		},
		{
			ID: "database-pool", Title: "Database Pool",
			Summary:  "Connection pooling.",
			Content:  "The database pool reuses connections. Auth queries also flow through the pool.",
			Concepts: []string{"database"}, Categories: []string{"Backend"},
		},
		{
			ID: "session-store", Title: "Session Store",
			Summary:  "Where sessions live.",
			Content:  "Sessions are stored in redis with a sliding TTL. The store handles eviction.",
			Concepts: []string{"sessions", "redis"}, Categories: []string{"Backend"},
		},
		{
			ID: "glossary-pocket", Title: "Pocket",
			Summary:  "Glossary: Pocket",
			Content:  "A Pocket is a workspace canvas.",
			Concepts: []string{"glossary"}, Kind: "glossary", Term: "Pocket",
			Aliases: []string{"canvas"},
		},
	}
}

func idsOf(articles []*WikiArticle) []string {
	ids := make([]string, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	return ids
}

func TestInvertedIndexMatchesSlowPath(t *testing.T) {
	articles := searchTestCorpus()
	si := buildSearchIndex(articles)
	if si.V != searchIndexVersion {
		t.Fatalf("buildSearchIndex version = %d, want %d", si.V, searchIndexVersion)
	}

	queries := []string{
		"auth",          // multi-doc term + title/concept boost on auth-middleware
		"sessions",      // appears in bodies and one title/concept set
		"database pool", // multi-term
		"pocket",        // glossary exact-Term boost
		"canvas",        // glossary alias-only boost (zero body TF elsewhere)
		"redis eviction TTL",
		"nonexistentterm",
		"auth auth sessions", // duplicate query terms accumulate per occurrence
	}
	for _, q := range queries {
		fast := bm25SearchWithIndex(articles, q, 10, si)
		slow := bm25SearchWithIndex(articles, q, 10, nil)
		fastIDs, slowIDs := idsOf(fast), idsOf(slow)
		if len(fastIDs) != len(slowIDs) {
			t.Fatalf("query %q: fast returned %v, slow returned %v", q, fastIDs, slowIDs)
		}
		for i := range fastIDs {
			if fastIDs[i] != slowIDs[i] {
				t.Errorf("query %q: rank %d differs — fast %v vs slow %v", q, i, fastIDs, slowIDs)
				break
			}
		}
	}
}

func TestSearchIndexV2RoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "sidx-" + filepath.Base(dir)
	ensureDirs(scope)

	articles := searchTestCorpus()
	if err := saveSearchIndex(scope, buildSearchIndex(articles)); err != nil {
		t.Fatalf("saveSearchIndex: %v", err)
	}
	si := loadSearchIndex(scope)
	if si == nil {
		t.Fatalf("loadSearchIndex returned nil for a freshly saved v2 index")
	}
	if !indexMatches(si, articles) {
		t.Fatalf("loaded index does not match the articles it was built from")
	}

	hits := bm25SearchWithIndex(articles, "auth", 5, si)
	if len(hits) == 0 || hits[0].ID != "auth-middleware" {
		t.Errorf("round-tripped index: query 'auth' top hit = %v, want auth-middleware", idsOf(hits))
	}
}

func TestLoadSearchIndexIgnoresOldFormat(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "sidx-old-" + filepath.Base(dir)
	ensureDirs(scope)

	// v1 token-dump shape: {"articles": [{"id", "all", "title", "concepts"}], "avg_dl"}
	old := `{"articles":[{"id":"a","all":["alpha","beta"],"title":["alpha"],"concepts":[]}],"avg_dl":2}`
	path := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatalf("write old index: %v", err)
	}

	if si := loadSearchIndex(scope); si != nil {
		t.Errorf("loadSearchIndex should return nil for an old-format index, got %+v", si)
	}

	// Search still works via the slow path with a nil index.
	articles := searchTestCorpus()
	hits := bm25SearchWithIndex(articles, "auth", 5, loadSearchIndex(scope))
	if len(hits) == 0 || hits[0].ID != "auth-middleware" {
		t.Errorf("slow-path fallback broken: got %v", idsOf(hits))
	}
}

// seedSearchCorpus persists searchTestCorpus into a scope so cmdSearch's real
// disk path (listArticles + index load) can run against it.
func seedSearchCorpus(t *testing.T, scope string) {
	t.Helper()
	for _, a := range searchTestCorpus() {
		if err := saveArticle(scope, a); err != nil {
			t.Fatalf("saveArticle %s: %v", a.ID, err)
		}
	}
}

func TestSearchSelfHealsIndex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "sidx-heal-" + filepath.Base(dir)
	ensureDirs(scope)
	seedSearchCorpus(t, scope)

	// Plant an old-format (v1) index — the shape a scope has right after
	// upgrading kb without re-ingesting anything.
	old := `{"articles":[{"id":"a","all":["alpha"],"title":["alpha"],"concepts":[]}],"avg_dl":1}`
	idxPath := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	if err := os.WriteFile(idxPath, []byte(old), 0o644); err != nil {
		t.Fatalf("write v1 index: %v", err)
	}
	if si := loadSearchIndex(scope); si != nil {
		t.Fatalf("precondition: v1 index should load as nil")
	}

	// First search: scores from articles (v1 index unusable) AND heals the
	// file to v2 as a side effect.
	cmdSearch([]string{"auth", "--scope", scope, "--json"})

	si := loadSearchIndex(scope)
	if si == nil {
		t.Fatalf("search did not heal the index: still unloadable after cmdSearch")
	}
	if si.V != searchIndexVersion {
		t.Fatalf("healed index version = %d, want %d", si.V, searchIndexVersion)
	}
	all, _ := listArticles(scope)
	if !indexMatches(si, all) {
		t.Fatalf("healed index does not match the scope's articles")
	}

	// Same for a missing index file.
	if err := os.Remove(idxPath); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	cmdSearch([]string{"auth", "--scope", scope, "--json"})
	if si := loadSearchIndex(scope); !indexMatches(si, all) {
		t.Fatalf("search did not heal a missing index")
	}
}

func TestSearchTagFilteredDoesNotClobberIndex(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "sidx-noclobber-" + filepath.Base(dir)
	ensureDirs(scope)
	seedSearchCorpus(t, scope)

	// No index on disk. A tag-filtered search scores a SLICE of the scope —
	// it must not persist an index describing that slice as the full scope.
	cmdSearch([]string{"auth", "--scope", scope, "--exclude-tags", "Backend", "--json"})
	idxPath := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	if _, err := os.Stat(idxPath); err == nil {
		t.Fatalf("tag-filtered search wrote a search index; it must not")
	}

	// loadOrHealSearchIndex on an empty article set must not write either
	// (searching a nonexistent scope stays a pure read).
	if si := loadOrHealSearchIndex("no-such-scope-xyz", nil); si != nil {
		t.Errorf("loadOrHealSearchIndex(empty) = %+v, want nil", si)
	}
	if _, err := os.Stat(filepath.Join(basePath(), "no-such-scope-xyz")); err == nil {
		t.Errorf("healing an empty scope created its directory")
	}
}

func TestStaleIndexNotTrusted(t *testing.T) {
	articles := searchTestCorpus()
	si := buildSearchIndex(articles[:2]) // index built before two docs were added

	if indexMatches(si, articles) {
		t.Fatalf("indexMatches accepted a stale index (2 docs indexed, 4 live)")
	}
	// Search with the stale index must still return correct results (slow path).
	hits := bm25SearchWithIndex(articles, "redis", 5, si)
	if len(hits) == 0 || hits[0].ID != "session-store" {
		t.Errorf("stale-index search: got %v, want session-store first", idsOf(hits))
	}

	// Same doc count but different ids — also rejected.
	si2 := buildSearchIndex(articles)
	swapped := []*WikiArticle{articles[1], articles[0], articles[2], articles[3]}
	if indexMatches(si2, swapped) {
		t.Errorf("indexMatches accepted an index whose doc order differs from the articles")
	}
}
