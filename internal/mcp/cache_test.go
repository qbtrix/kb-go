// cache_test.go — Cross-process write correctness + cost of `kb serve`.
//
// The MCP server is long-lived while other processes (`kb ingest`, `build`,
// `accept`, `recompile`, `delete`, `clear`, convo ingest) write the same scope.
// These tests drive ONE Server instance and interleave on-disk writes made
// the way those commands make them, pinning that the very next tool call sees
// every add / overwrite / delete / clear — including writes that touch only
// wiki/*.md (convo ingest, category normalization) and an overwrite that keeps
// the same size and the same mtime (a write inside one filesystem clock tick).
// Seeded files are backdated so the server's article cache treats them as
// settled and the tests exercise the reuse path, not just the cold path.
//
// BenchmarkMCPSearch measures per-call kb_search cost on a ~300-article scope
// through the registered tool handler (no stdio framing), so before/after
// numbers isolate the article-loading cost.
package mcp

import (
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/search"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// cacheTestArticle builds a small article whose content carries a unique word.
func cacheTestArticle(id, title, word string) *model.WikiArticle {
	return &model.WikiArticle{
		ID: id, Title: title,
		Summary:    "Summary for " + title,
		Content:    "# " + title + "\n\nThis article discusses " + word + " in depth.",
		Concepts:   []string{"testing"},
		Categories: []string{"cache"},
		WordCount:  10, CompiledWith: "test", Version: 1,
	}
}

// cacheTestScope creates a fresh scope with three articles, an index and a
// search index (as `kb build` leaves it), then backdates every file so it is
// well outside any racy-mtime window.
func cacheTestScope(t *testing.T, suffix string) string {
	t.Helper()
	scope := "test-mcpcache-" + suffix + "-" + textutil.ContentHash(t.Name())[:8]
	os.RemoveAll(store.ScopeDir(scope))
	t.Cleanup(func() { os.RemoveAll(store.ScopeDir(scope)) })
	for _, a := range []*model.WikiArticle{
		cacheTestArticle("alpha", "Alpha", "aardvark"),
		cacheTestArticle("beta", "Beta", "buffalo"),
		cacheTestArticle("gamma", "Gamma", "gazelle"),
	} {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatal(err)
		}
	}
	cliRebuild(t, scope)
	backdateScope(t, scope, time.Hour)
	return scope
}

// cliRebuild refreshes index.json + the search index from the wiki files —
// what finishIngest / build / accept / recompile do after writing articles.
func cliRebuild(t *testing.T, scope string) {
	t.Helper()
	all, _ := store.ListArticles(scope)
	if err := store.SaveIndex(scope, store.RebuildIndex(scope, all)); err != nil {
		t.Fatal(err)
	}
	if err := search.SaveIndex(scope, search.BuildIndex(all)); err != nil {
		t.Fatal(err)
	}
}

// backdateScope sets every file under the scope to now-age.
func backdateScope(t *testing.T, scope string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	filepath.Walk(store.ScopeDir(scope), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			os.Chtimes(p, old, old)
		}
		return nil
	})
}

// toolRows calls a registered tool handler directly and returns its rows.
func toolRows(t *testing.T, srv *Server, tool string, args map[string]any) []map[string]any {
	t.Helper()
	out, err := srv.funcs[tool](args)
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	rows, ok := out.([]map[string]any)
	if !ok {
		t.Fatalf("%s: unexpected payload %T", tool, out)
	}
	return rows
}

func searchIDs(t *testing.T, srv *Server, scope, query string) []string {
	t.Helper()
	var ids []string
	for _, r := range toolRows(t, srv, "kb_search", map[string]any{"query": query, "scope": scope, "limit": 50}) {
		ids = append(ids, r["id"].(string))
	}
	return ids
}

// listTitles maps id -> title from kb_list.
func listTitles(t *testing.T, srv *Server, scope string) map[string]string {
	t.Helper()
	m := map[string]string{}
	for _, r := range toolRows(t, srv, "kb_list", map[string]any{"scope": scope}) {
		m[r["id"].(string)] = r["title"].(string)
	}
	return m
}

func hasID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func newCacheTestServer(scope string) *Server {
	return NewServer(strings.NewReader(""), &strings.Builder{}, scope)
}

func TestMCPCacheSeesNewArticle(t *testing.T) {
	scope := cacheTestScope(t, "add")
	srv := newCacheTestServer(scope)

	if ids := searchIDs(t, srv, scope, "aardvark"); !hasID(ids, "alpha") {
		t.Fatalf("warm-up search missed alpha: %v", ids)
	}
	if ids := searchIDs(t, srv, scope, "dolphin"); len(ids) != 0 {
		t.Fatalf("dolphin should not match yet: %v", ids)
	}

	// Another process ingests a new article (full CLI write path).
	if err := store.SaveArticle(scope, cacheTestArticle("delta", "Delta", "dolphin")); err != nil {
		t.Fatal(err)
	}
	cliRebuild(t, scope)
	if ids := searchIDs(t, srv, scope, "dolphin"); !hasID(ids, "delta") {
		t.Fatalf("search after ingest missed new article delta: %v", ids)
	}

	// Wiki-only writer (convo ingest): no index.json / search index refresh.
	if err := store.SaveArticle(scope, cacheTestArticle("epsilon", "Epsilon", "elephant")); err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(t, srv, scope, "elephant"); !hasID(ids, "epsilon") {
		t.Fatalf("search after wiki-only write missed epsilon: %v", ids)
	}
	if titles := listTitles(t, srv, scope); titles["epsilon"] != "Epsilon" || len(titles) != 5 {
		t.Fatalf("kb_list after adds = %v", titles)
	}
	stats, _ := srv.funcs["kb_stats"](map[string]any{"scope": scope})
	if n := stats.(map[string]any)["articles"]; n != 5 {
		t.Fatalf("kb_stats articles = %v, want 5", n)
	}
}

func TestMCPCacheSeesOverwrite(t *testing.T) {
	scope := cacheTestScope(t, "overwrite")
	srv := newCacheTestServer(scope)

	if ids := searchIDs(t, srv, scope, "buffalo"); !hasID(ids, "beta") {
		t.Fatalf("warm-up search missed beta: %v", ids)
	}
	listTitles(t, srv, scope)

	// Recompile-style overwrite: new content + index rebuild.
	b := cacheTestArticle("beta", "Beta Revised", "bison")
	b.Version = 2
	if err := store.SaveArticle(scope, b); err != nil {
		t.Fatal(err)
	}
	cliRebuild(t, scope)
	if ids := searchIDs(t, srv, scope, "bison"); !hasID(ids, "beta") {
		t.Fatalf("search after overwrite missed new content: %v", ids)
	}
	if ids := searchIDs(t, srv, scope, "buffalo"); hasID(ids, "beta") {
		t.Fatalf("search after overwrite still matched old content: %v", ids)
	}
	if got := listTitles(t, srv, scope)["beta"]; got != "Beta Revised" {
		t.Fatalf("kb_list title after overwrite = %q", got)
	}

	// Wiki-only overwrite (category normalization rewrites the file only).
	b.Title = "Beta Normalized"
	if err := store.SaveArticle(scope, b); err != nil {
		t.Fatal(err)
	}
	if got := listTitles(t, srv, scope)["beta"]; got != "Beta Normalized" {
		t.Fatalf("kb_list title after wiki-only overwrite = %q", got)
	}
	rows := toolRows(t, srv, "kb_search", map[string]any{"query": "bison", "scope": scope})
	if len(rows) == 0 || rows[0]["title"] != "Beta Normalized" {
		t.Fatalf("kb_search returned stale title after wiki-only overwrite: %v", rows)
	}
}

// An overwrite inside one filesystem clock tick can leave size AND mtime
// unchanged. A file read that close to its mtime must not be trusted.
func TestMCPCacheSeesSameSizeSameMtimeOverwrite(t *testing.T) {
	scope := cacheTestScope(t, "racy")
	srv := newCacheTestServer(scope)

	// Fresh write (mtime ~ now), read immediately by the server.
	if err := store.SaveArticle(scope, cacheTestArticle("gamma", "Gamma One", "gazelle")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.ScopeDir(scope), "wiki", "gamma.md")
	before, _ := os.Stat(path)
	if got := listTitles(t, srv, scope)["gamma"]; got != "Gamma One" {
		t.Fatalf("title = %q", got)
	}

	// Same-length rewrite, then pin the mtime back to the original value.
	if err := store.SaveArticle(scope, cacheTestArticle("gamma", "Gamma Two", "gazelle")); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(path, before.ModTime(), before.ModTime())
	after, _ := os.Stat(path)
	if after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatalf("setup: stat changed (size %d->%d)", before.Size(), after.Size())
	}
	if got := listTitles(t, srv, scope)["gamma"]; got != "Gamma Two" {
		t.Fatalf("same-size same-mtime overwrite served stale title %q", got)
	}
}

func TestMCPCacheDropsDeleted(t *testing.T) {
	scope := cacheTestScope(t, "delete")
	srv := newCacheTestServer(scope)

	if ids := searchIDs(t, srv, scope, "aardvark"); !hasID(ids, "alpha") {
		t.Fatalf("warm-up search missed alpha: %v", ids)
	}
	// Raw file removal, no index refresh.
	if err := os.Remove(filepath.Join(store.ScopeDir(scope), "wiki", "alpha.md")); err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(t, srv, scope, "aardvark"); hasID(ids, "alpha") {
		t.Fatalf("search returned removed article alpha: %v", ids)
	}

	// `kb delete`, run as the real binary (another process, as in production).
	if ids := searchIDs(t, srv, scope, "buffalo"); !hasID(ids, "beta") {
		t.Fatalf("warm-up search missed beta: %v", ids)
	}
	kbExec(t, "delete", "beta", "--scope", scope, "--json")
	if ids := searchIDs(t, srv, scope, "buffalo"); hasID(ids, "beta") {
		t.Fatalf("search returned kb-deleted article beta: %v", ids)
	}
	if titles := listTitles(t, srv, scope); len(titles) != 1 || titles["gamma"] == "" {
		t.Fatalf("kb_list after deletes = %v", titles)
	}
	if _, err := srv.funcs["kb_show"](map[string]any{"id": "beta", "scope": scope}); err == nil {
		t.Fatalf("kb_show returned deleted article beta")
	}
}

func TestMCPCacheClearScope(t *testing.T) {
	scope := cacheTestScope(t, "clear")
	srv := newCacheTestServer(scope)

	if ids := searchIDs(t, srv, scope, "gazelle"); !hasID(ids, "gamma") {
		t.Fatalf("warm-up search missed gamma: %v", ids)
	}
	kbExec(t, "clear", "--scope", scope, "--json")
	if ids := searchIDs(t, srv, scope, "gazelle aardvark buffalo"); len(ids) != 0 {
		t.Fatalf("search after clear returned %v", ids)
	}
	if titles := listTitles(t, srv, scope); len(titles) != 0 {
		t.Fatalf("kb_list after clear = %v", titles)
	}

	// Scope directory removed entirely, then repopulated.
	os.RemoveAll(store.ScopeDir(scope))
	if ids := searchIDs(t, srv, scope, "gazelle"); len(ids) != 0 {
		t.Fatalf("search on removed scope returned %v", ids)
	}
	if err := store.SaveArticle(scope, cacheTestArticle("zeta", "Zeta", "zebra")); err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(t, srv, scope, "zebra"); !hasID(ids, "zeta") {
		t.Fatalf("search after repopulate missed zeta: %v", ids)
	}
}

func TestMCPCacheMultiScope(t *testing.T) {
	a := cacheTestScope(t, "ma")
	b := cacheTestScope(t, "mb")
	srv := newCacheTestServer(a)
	multi := a + "," + b

	rows := toolRows(t, srv, "kb_search", map[string]any{"query": "aardvark", "scope": multi, "limit": 10})
	if len(rows) != 2 {
		t.Fatalf("multi-scope warm-up: want 2 rows, got %v", rows)
	}
	if err := store.SaveArticle(b, cacheTestArticle("eta", "Eta", "emu")); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(store.ScopeDir(a), "wiki", "alpha.md"))

	rows = toolRows(t, srv, "kb_search", map[string]any{"query": "emu aardvark", "scope": multi, "limit": 10})
	got := map[string]string{}
	for _, r := range rows {
		got[r["id"].(string)+"@"+r["scope"].(string)] = r["title"].(string)
	}
	want := []string{"eta@" + b, "alpha@" + b}
	if len(got) != len(want) {
		t.Fatalf("multi-scope after writes: got %v, want %v", got, want)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("multi-scope after writes: got %v, missing %s", got, k)
		}
	}
}

// --- benchmark ---

// seedBenchScope writes n articles of a few KB each plus index + search index,
// backdated so a cache may treat them as settled.
func seedBenchScope(b *testing.B, n int) string {
	b.Helper()
	scope := fmt.Sprintf("bench-mcpcache-%d", n)
	os.RemoveAll(store.ScopeDir(scope))
	b.Cleanup(func() { os.RemoveAll(store.ScopeDir(scope)) })
	corpus := generateCorpus(n)
	for _, a := range corpus {
		a.Content = strings.Repeat(a.Content+"\n\n", 8) // ~5-10 KB per article
		if err := store.SaveArticle(scope, a); err != nil {
			b.Fatal(err)
		}
	}
	all, _ := store.ListArticles(scope)
	store.SaveIndex(scope, store.RebuildIndex(scope, all))
	search.SaveIndex(scope, search.BuildIndex(all))
	old := time.Now().Add(-time.Hour)
	filepath.Walk(store.ScopeDir(scope), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			os.Chtimes(p, old, old)
		}
		return nil
	})
	return scope
}

func BenchmarkMCPSearch(b *testing.B) {
	scope := seedBenchScope(b, 300)
	srv := NewServer(strings.NewReader(""), &strings.Builder{}, scope)
	h := srv.funcs["kb_search"]
	queries := []string{"async database", "middleware routing", "cache queue handler", "encrypted storage"}
	if _, err := h(map[string]any{"query": "warm", "scope": scope}); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := h(map[string]any{"query": queries[i%len(queries)], "scope": scope}); err != nil {
			b.Fatal(err)
		}
	}
}

// The cache must actually reuse settled files: two calls with no writes return
// the same parsed articles, and a write re-parses only the touched file.
func TestArticleCacheReusesSettledFiles(t *testing.T) {
	scope := cacheTestScope(t, "reuse")
	c := newArticleCache()
	first, _ := c.list(scope)
	second, _ := c.list(scope)
	if len(first) != 3 || &first[0] != &second[0] {
		t.Fatalf("unchanged scope rebuilt the article slice")
	}
	c.search(scope, "alpha", 5, "")
	si := c.indexes[store.ScopeDir(scope)]
	c.search(scope, "alpha", 5, "")
	if si == nil || c.indexes[store.ScopeDir(scope)] != si {
		t.Fatalf("unchanged search index was decoded twice")
	}

	if err := store.SaveArticle(scope, cacheTestArticle("beta", "Beta Two", "bison")); err != nil {
		t.Fatal(err)
	}
	third, _ := c.list(scope)
	if third[0] != first[0] || third[2] != first[2] {
		t.Fatalf("untouched articles were re-parsed")
	}
	if third[1] == first[1] || third[1].Title != "Beta Two" {
		t.Fatalf("touched article not re-parsed: %q", third[1].Title)
	}
}

// Cached search must rank exactly like the uncached CLI path (store.ListArticles +
// search.LoadOrHealIndex), including ids whose file-name order differs from ID
// order ("a-b.md" sorts before "a.md", but ID "a" sorts before "a-b"), so the
// search.Index docIdx stays aligned with the cached slice.
func TestMCPCacheSearchMatchesUncachedPath(t *testing.T) {
	scope := cacheTestScope(t, "parity")
	for _, a := range []*model.WikiArticle{
		cacheTestArticle("a", "A", "otter middleware"),
		cacheTestArticle("a-b", "A B", "otter otter routing"),
		cacheTestArticle("a_c", "A C", "middleware routing"),
	} {
		store.SaveArticle(scope, a)
	}
	cliRebuild(t, scope)
	backdateScope(t, scope, time.Hour)
	srv := newCacheTestServer(scope)

	check := func(stage string) {
		t.Helper()
		for _, q := range []string{"otter", "middleware", "routing otter", "testing", "aardvark gazelle"} {
			got := searchIDs(t, srv, scope, q)
			all, _ := store.ListArticles(scope)
			var want []string
			for _, a := range search.BM25WithIndex(all, q, 50, search.LoadOrHealIndex(scope, all)) {
				want = append(want, a.ID)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s %q: cached %v, uncached %v", stage, q, got, want)
			}
		}
	}
	check("settled")
	store.SaveArticle(scope, cacheTestArticle("a-a", "A A", "otter otter otter"))
	check("after wiki-only add")
	os.Remove(filepath.Join(store.ScopeDir(scope), "wiki", "a.md"))
	cliRebuild(t, scope)
	check("after remove + rebuild")
}

// kbExec runs the real kb binary against the same home, the way another
// process mutates a scope while the server is running.
func kbExec(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command(kbtest.BuildBinary(t), args...).CombinedOutput(); err != nil {
		t.Fatalf("kb %v: %v\n%s", args, err, out)
	}
}

// generateCorpus creates n synthetic WikiArticles with realistic content.
func generateCorpus(n int) []*model.WikiArticle {
	rng := rand.New(rand.NewSource(42)) // deterministic
	domains := []string{"authentication", "database", "routing", "middleware", "config",
		"logging", "cache", "queue", "storage", "api", "service", "handler",
		"model", "controller", "repository", "factory", "builder", "observer"}
	adjectives := []string{"async", "distributed", "concurrent", "stateless", "encrypted",
		"cached", "batched", "streaming", "reactive", "immutable"}

	articles := make([]*model.WikiArticle, n)
	for i := 0; i < n; i++ {
		domain := domains[rng.Intn(len(domains))]
		adj := adjectives[rng.Intn(len(adjectives))]
		title := fmt.Sprintf("%s %s %d", adj, domain, i)

		// Generate realistic content (50-200 words)
		wordCount := 50 + rng.Intn(150)
		words := make([]string, wordCount)
		vocab := append(domains, adjectives...)
		vocab = append(vocab, "the", "a", "an", "is", "are", "was", "with", "for",
			"and", "or", "to", "from", "in", "on", "by", "this", "that",
			"function", "class", "method", "struct", "interface", "type",
			"returns", "handles", "processes", "manages", "creates", "deletes")
		for j := range words {
			words[j] = vocab[rng.Intn(len(vocab))]
		}

		concepts := []string{domain, adj}
		if rng.Float64() > 0.5 {
			concepts = append(concepts, domains[rng.Intn(len(domains))])
		}

		articles[i] = &model.WikiArticle{
			ID:         textutil.Slugify(title),
			Title:      title,
			Summary:    fmt.Sprintf("Article about %s %s patterns", adj, domain),
			Content:    strings.Join(words, " "),
			Concepts:   concepts,
			Categories: []string{domain},
			WordCount:  wordCount,
			Version:    1,
		}
	}
	return articles
}
