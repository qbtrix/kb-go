// search_index_test.go — `kb search` and the persisted search index: a search
// self-heals a missing or old-format index (a current-version .bin appears,
// matches the scope, and the legacy search_index.json is removed); a
// tag-filtered search that heals writes the FULL scope's index, never the
// filtered slice; empty scopes never write one. The index itself is tested in
// internal/search.

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

	// Plant an old-format (v1) JSON index — the shape a scope has right after
	// upgrading kb without re-ingesting anything.
	old := `{"articles":[{"id":"a","all":["alpha"],"title":["alpha"],"concepts":[]}],"avg_dl":1}`
	jsonPath := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	if err := os.WriteFile(jsonPath, []byte(old), 0o644); err != nil {
		t.Fatalf("write v1 index: %v", err)
	}
	if si := search.LoadIndex(scope); si != nil {
		t.Fatalf("precondition: no binary index should load")
	}

	// First search: heals the index to the current version as a side effect
	// and drops the legacy JSON file.
	cmdSearch([]string{"auth", "--scope", scope, "--json"})
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("healing left the legacy search_index.json behind (err=%v)", err)
	}

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
	if err := os.Remove(search.IndexPath(scope)); err != nil {
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

	// No index on disk. A tag-filtered search scores a SLICE of the scope; if
	// it heals the index, the file must describe the FULL scope, not the slice.
	cmdSearch([]string{"auth", "--scope", scope, "--exclude-tags", "Backend", "--json"})
	if si := search.LoadIndex(scope); si != nil {
		all, _ := store.ListArticles(scope)
		if !search.IndexMatches(si, all) {
			t.Fatalf("tag-filtered search persisted an index of %v, want the full scope", si.DocIDs)
		}
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

// A wiki-only rewrite under the same id (convo ingest, lint
// --normalize-categories --apply) keeps every id, so an id-only staleness
// check trusted the old index and searched the old content. Search must see
// the new content.
func TestSearchSeesSameIDRewrite(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "sidx-rewrite-" + filepath.Base(dir)
	store.EnsureDirs(scope)
	seedSearchCorpus(t, scope)
	all, _ := store.ListArticles(scope)
	if err := search.SaveIndex(scope, search.BuildIndex(all)); err != nil {
		t.Fatal(err)
	}
	pool := all[1]
	pool.Content = "Quokkas guard the connection pool now."
	if err := store.SaveArticle(scope, pool); err != nil { // wiki only, no index write
		t.Fatal(err)
	}

	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	cmdSearch([]string{"quokkas", "--scope", scope, "--json"})
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if !strings.Contains(buf.String(), `"database-pool"`) {
		t.Fatalf("search missed content rewritten under the same id; got %s", buf.String())
	}
}
