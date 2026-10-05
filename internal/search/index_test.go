// Tests for the inverted search index: the postings fast path ranks exactly like
// the on-the-fly slow path (plain, title, concept and glossary boosted
// queries), save/load round trips, an old-format file is ignored on load, and a
// stale index (doc set changed without a rebuild) is never trusted.

package search

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
)

func searchTestCorpus() []*model.WikiArticle {
	return []*model.WikiArticle{
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

func idsOf(articles []*model.WikiArticle) []string {
	ids := make([]string, len(articles))
	for i, a := range articles {
		ids[i] = a.ID
	}
	return ids
}

func TestInvertedIndexMatchesSlowPath(t *testing.T) {
	articles := searchTestCorpus()
	si := BuildIndex(articles)
	if si.V != IndexVersion {
		t.Fatalf("buildSearchIndex version = %d, want %d", si.V, IndexVersion)
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
		fast := BM25WithIndex(articles, q, 10, si)
		slow := BM25WithIndex(articles, q, 10, nil)
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

func TestSearchIndexRoundTrip(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "sidx-" + filepath.Base(dir)
	store.EnsureDirs(scope)

	articles := searchTestCorpus()
	if err := SaveIndex(scope, BuildIndex(articles)); err != nil {
		t.Fatalf("saveSearchIndex: %v", err)
	}
	si := LoadIndex(scope)
	if si == nil {
		t.Fatalf("loadSearchIndex returned nil for a freshly saved current-version index")
	}
	if !IndexMatches(si, articles) {
		t.Fatalf("loaded index does not match the articles it was built from")
	}

	hits := BM25WithIndex(articles, "auth", 5, si)
	if len(hits) == 0 || hits[0].ID != "auth-middleware" {
		t.Errorf("round-tripped index: query 'auth' top hit = %v, want auth-middleware", idsOf(hits))
	}
}

func TestLoadSearchIndexIgnoresOldFormat(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "sidx-old-" + filepath.Base(dir)
	store.EnsureDirs(scope)

	// v1 token-dump shape: {"articles": [{"id", "all", "title", "concepts"}], "avg_dl"}
	old := `{"articles":[{"id":"a","all":["alpha","beta"],"title":["alpha"],"concepts":[]}],"avg_dl":2}`
	path := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatalf("write old index: %v", err)
	}

	if si := LoadIndex(scope); si != nil {
		t.Errorf("loadSearchIndex should return nil for an old-format index, got %+v", si)
	}

	// Search still works via the slow path with a nil index.
	articles := searchTestCorpus()
	hits := BM25WithIndex(articles, "auth", 5, LoadIndex(scope))
	if len(hits) == 0 || hits[0].ID != "auth-middleware" {
		t.Errorf("slow-path fallback broken: got %v", idsOf(hits))
	}
}

func TestStaleIndexNotTrusted(t *testing.T) {
	articles := searchTestCorpus()
	si := BuildIndex(articles[:2]) // index built before two docs were added

	if IndexMatches(si, articles) {
		t.Fatalf("indexMatches accepted a stale index (2 docs indexed, 4 live)")
	}
	// Search with the stale index must still return correct results (slow path).
	hits := BM25WithIndex(articles, "redis", 5, si)
	if len(hits) == 0 || hits[0].ID != "session-store" {
		t.Errorf("stale-index search: got %v, want session-store first", idsOf(hits))
	}

	// Same doc count but different ids — also rejected.
	si2 := BuildIndex(articles)
	swapped := []*model.WikiArticle{articles[1], articles[0], articles[2], articles[3]}
	if IndexMatches(si2, swapped) {
		t.Errorf("indexMatches accepted an index whose doc order differs from the articles")
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
