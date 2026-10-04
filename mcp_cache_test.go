// mcp_cache_test.go — Cross-process write correctness + cost of `kb serve`.
//
// The MCP server is long-lived while other processes (`kb ingest`, `build`,
// `accept`, `recompile`, `delete`, `clear`, convo ingest) write the same scope.
// These tests drive ONE mcpServer instance and interleave on-disk writes made
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
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// cacheTestArticle builds a small article whose content carries a unique word.
func cacheTestArticle(id, title, word string) *WikiArticle {
	return &WikiArticle{
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
	scope := "test-mcpcache-" + suffix + "-" + contentHash(t.Name())[:8]
	os.RemoveAll(scopeDir(scope))
	t.Cleanup(func() { os.RemoveAll(scopeDir(scope)) })
	for _, a := range []*WikiArticle{
		cacheTestArticle("alpha", "Alpha", "aardvark"),
		cacheTestArticle("beta", "Beta", "buffalo"),
		cacheTestArticle("gamma", "Gamma", "gazelle"),
	} {
		if err := saveArticle(scope, a); err != nil {
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
	all, _ := listArticles(scope)
	if err := saveIndex(scope, rebuildIndex(scope, all)); err != nil {
		t.Fatal(err)
	}
	if err := saveSearchIndex(scope, buildSearchIndex(all)); err != nil {
		t.Fatal(err)
	}
}

// backdateScope sets every file under the scope to now-age.
func backdateScope(t *testing.T, scope string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	filepath.Walk(scopeDir(scope), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			os.Chtimes(p, old, old)
		}
		return nil
	})
}

// toolRows calls a registered tool handler directly and returns its rows.
func toolRows(t *testing.T, srv *mcpServer, tool string, args map[string]any) []map[string]any {
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

func searchIDs(t *testing.T, srv *mcpServer, scope, query string) []string {
	t.Helper()
	var ids []string
	for _, r := range toolRows(t, srv, "kb_search", map[string]any{"query": query, "scope": scope, "limit": 50}) {
		ids = append(ids, r["id"].(string))
	}
	return ids
}

// listTitles maps id -> title from kb_list.
func listTitles(t *testing.T, srv *mcpServer, scope string) map[string]string {
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

func newCacheTestServer(scope string) *mcpServer {
	return newMCPServer(strings.NewReader(""), &strings.Builder{}, scope)
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
	if err := saveArticle(scope, cacheTestArticle("delta", "Delta", "dolphin")); err != nil {
		t.Fatal(err)
	}
	cliRebuild(t, scope)
	if ids := searchIDs(t, srv, scope, "dolphin"); !hasID(ids, "delta") {
		t.Fatalf("search after ingest missed new article delta: %v", ids)
	}

	// Wiki-only writer (convo ingest): no index.json / search index refresh.
	if err := saveArticle(scope, cacheTestArticle("epsilon", "Epsilon", "elephant")); err != nil {
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
	if err := saveArticle(scope, b); err != nil {
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
	if err := saveArticle(scope, b); err != nil {
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
	if err := saveArticle(scope, cacheTestArticle("gamma", "Gamma One", "gazelle")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(scopeDir(scope), "wiki", "gamma.md")
	before, _ := os.Stat(path)
	if got := listTitles(t, srv, scope)["gamma"]; got != "Gamma One" {
		t.Fatalf("title = %q", got)
	}

	// Same-length rewrite, then pin the mtime back to the original value.
	if err := saveArticle(scope, cacheTestArticle("gamma", "Gamma Two", "gazelle")); err != nil {
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
	if err := os.Remove(filepath.Join(scopeDir(scope), "wiki", "alpha.md")); err != nil {
		t.Fatal(err)
	}
	if ids := searchIDs(t, srv, scope, "aardvark"); hasID(ids, "alpha") {
		t.Fatalf("search returned removed article alpha: %v", ids)
	}

	// `kb delete` (in-process, same code path as the CLI).
	if ids := searchIDs(t, srv, scope, "buffalo"); !hasID(ids, "beta") {
		t.Fatalf("warm-up search missed beta: %v", ids)
	}
	cmdDelete([]string{"beta", "--scope", scope, "--json"})
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
	cmdClear([]string{"--scope", scope, "--json"})
	if ids := searchIDs(t, srv, scope, "gazelle aardvark buffalo"); len(ids) != 0 {
		t.Fatalf("search after clear returned %v", ids)
	}
	if titles := listTitles(t, srv, scope); len(titles) != 0 {
		t.Fatalf("kb_list after clear = %v", titles)
	}

	// Scope directory removed entirely, then repopulated.
	os.RemoveAll(scopeDir(scope))
	if ids := searchIDs(t, srv, scope, "gazelle"); len(ids) != 0 {
		t.Fatalf("search on removed scope returned %v", ids)
	}
	if err := saveArticle(scope, cacheTestArticle("zeta", "Zeta", "zebra")); err != nil {
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
	if err := saveArticle(b, cacheTestArticle("eta", "Eta", "emu")); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(scopeDir(a), "wiki", "alpha.md"))

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
	os.RemoveAll(scopeDir(scope))
	b.Cleanup(func() { os.RemoveAll(scopeDir(scope)) })
	corpus := generateCorpus(n)
	for _, a := range corpus {
		a.Content = strings.Repeat(a.Content+"\n\n", 8) // ~5-10 KB per article
		if err := saveArticle(scope, a); err != nil {
			b.Fatal(err)
		}
	}
	all, _ := listArticles(scope)
	saveIndex(scope, rebuildIndex(scope, all))
	saveSearchIndex(scope, buildSearchIndex(all))
	old := time.Now().Add(-time.Hour)
	filepath.Walk(scopeDir(scope), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			os.Chtimes(p, old, old)
		}
		return nil
	})
	return scope
}

func BenchmarkMCPSearch(b *testing.B) {
	scope := seedBenchScope(b, 300)
	srv := newMCPServer(strings.NewReader(""), &strings.Builder{}, scope)
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
	if c.searchIndex(scope) != c.searchIndex(scope) {
		t.Fatalf("unchanged search index was decoded twice")
	}

	if err := saveArticle(scope, cacheTestArticle("beta", "Beta Two", "bison")); err != nil {
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

// Cached search must rank exactly like the uncached CLI path (listArticles +
// loadOrHealSearchIndex), including ids whose file-name order differs from ID
// order ("a-b.md" sorts before "a.md", but ID "a" sorts before "a-b"), so the
// SearchIndex docIdx stays aligned with the cached slice.
func TestMCPCacheSearchMatchesUncachedPath(t *testing.T) {
	scope := cacheTestScope(t, "parity")
	for _, a := range []*WikiArticle{
		cacheTestArticle("a", "A", "otter middleware"),
		cacheTestArticle("a-b", "A B", "otter otter routing"),
		cacheTestArticle("a_c", "A C", "middleware routing"),
	} {
		saveArticle(scope, a)
	}
	cliRebuild(t, scope)
	backdateScope(t, scope, time.Hour)
	srv := newCacheTestServer(scope)

	check := func(stage string) {
		t.Helper()
		for _, q := range []string{"otter", "middleware", "routing otter", "testing", "aardvark gazelle"} {
			got := searchIDs(t, srv, scope, q)
			all, _ := listArticles(scope)
			var want []string
			for _, a := range bm25SearchWithIndex(all, q, 50, loadOrHealSearchIndex(scope, all)) {
				want = append(want, a.ID)
			}
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("%s %q: cached %v, uncached %v", stage, q, got, want)
			}
		}
	}
	check("settled")
	saveArticle(scope, cacheTestArticle("a-a", "A A", "otter otter otter"))
	check("after wiki-only add")
	os.Remove(filepath.Join(scopeDir(scope), "wiki", "a.md"))
	cliRebuild(t, scope)
	check("after remove + rebuild")
}
