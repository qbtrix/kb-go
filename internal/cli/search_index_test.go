// search_index_test.go — `kb search` and the persisted search index: a
// full-scope search self-heals a missing or old-format index (the file
// reappears at the current version and matches the scope), while tag-filtered
// searches and empty scopes never write one. The index itself is tested in
// internal/search.

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/search"
	"github.com/qbtrix/kb-go/internal/store"
)

// seedSearchCorpus persists searchTestCorpus into a scope so cmdSearch's real
// disk path (store.ListArticles + index load) can run against it.
func seedSearchCorpus(t *testing.T, scope string) {
	t.Helper()
	for _, a := range searchTestCorpus() {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatalf("saveArticle %s: %v", a.ID, err)
		}
	}
}

func TestSearchSelfHealsIndex(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "sidx-heal-" + filepath.Base(dir)
	store.EnsureDirs(scope)
	seedSearchCorpus(t, scope)

	// Plant an old-format (v1) index — the shape a scope has right after
	// upgrading kb without re-ingesting anything.
	old := `{"articles":[{"id":"a","all":["alpha"],"title":["alpha"],"concepts":[]}],"avg_dl":1}`
	idxPath := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	if err := os.WriteFile(idxPath, []byte(old), 0o644); err != nil {
		t.Fatalf("write v1 index: %v", err)
	}
	if si := search.LoadIndex(scope); si != nil {
		t.Fatalf("precondition: v1 index should load as nil")
	}

	// First search: scores from articles (v1 index unusable) AND heals the
	// file to the current version as a side effect.
	cmdSearch([]string{"auth", "--scope", scope, "--json"})

	si := search.LoadIndex(scope)
	if si == nil {
		t.Fatalf("search did not heal the index: still unloadable after cmdSearch")
	}
	if si.V != search.IndexVersion {
		t.Fatalf("healed index version = %d, want %d", si.V, search.IndexVersion)
	}
	all, _ := store.ListArticles(scope)
	if !search.IndexMatches(si, all) {
		t.Fatalf("healed index does not match the scope's articles")
	}

	// Same for a missing index file.
	if err := os.Remove(idxPath); err != nil {
		t.Fatalf("remove index: %v", err)
	}
	cmdSearch([]string{"auth", "--scope", scope, "--json"})
	if si := search.LoadIndex(scope); !search.IndexMatches(si, all) {
		t.Fatalf("search did not heal a missing index")
	}
}

func TestSearchTagFilteredDoesNotClobberIndex(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "sidx-noclobber-" + filepath.Base(dir)
	store.EnsureDirs(scope)
	seedSearchCorpus(t, scope)

	// No index on disk. A tag-filtered search scores a SLICE of the scope —
	// it must not persist an index describing that slice as the full scope.
	cmdSearch([]string{"auth", "--scope", scope, "--exclude-tags", "Backend", "--json"})
	idxPath := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	if _, err := os.Stat(idxPath); err == nil {
		t.Fatalf("tag-filtered search wrote a search index; it must not")
	}

	// search.LoadOrHealIndex on an empty article set must not write either
	// (searching a nonexistent scope stays a pure read).
	if si := search.LoadOrHealIndex("no-such-scope-xyz", nil); si != nil {
		t.Errorf("loadOrHealSearchIndex(empty) = %+v, want nil", si)
	}
	if _, err := os.Stat(filepath.Join(store.BaseDir(), "no-such-scope-xyz")); err == nil {
		t.Errorf("healing an empty scope created its directory")
	}
}

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
