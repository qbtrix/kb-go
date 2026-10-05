// Tests for the binary search index (IndexVersion) and the index-only
// single-scope search path: the streaming tokenizer equals Tokenize;
// SearchScope ranks ids AND scores exactly like the frozen v3 pipeline
// (legacy_ref_test.go) over generated corpora, including glossary boosts,
// --exclude-tags, limit edges, empty and no-match queries; a v3 JSON index
// heals to a current .bin; a fresh-looking v4 .bin (full-Porter terms) is
// rejected and rebuilt; a missing,
// corrupt or truncated .bin falls back to a rebuild without panicking; and a
// stale doc table (file added, removed, rewritten under the same id, even with
// the same mtime and size) is never trusted.

package search

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
)

func TestStreamTokenizerMatchesTokenize(t *testing.T) {
	corpus := []string{
		"", " ", "a", "I am OK", "an is to by", "Opens OPENING opened opener",
		"utf8 v2 3.14 1,000 0x1F 42nd 2024-01-01T10:00Z",
		"don't stop-believing; e-mail @user #tag (paren) [b] {c} <d> a/b\\c",
		"naïve café résumé Ünïcödé façade",
		"İstanbul DİYARBAKIR ıi", // Turkish dotted/dotless i
		"Straße STRASSE ß ẞ",
		"ΣΊΣΥΦΟΣ σίσυφος ς",            // final sigma
		"\u212a\u212b Kelvin Ångström", // Kelvin sign lowercases to ASCII k
		"日本語のテキスト 中文 한국어",
		"emoji 😀 rocket🚀ship ✓done",
		"e\u0301 combining a\u0308 marks",
		"bad \xff\xfe utf8 \xc3 tail\xe2\x82",
		"tab\tnew\nline\r\ncrlf\u00a0nbsp\u2003emsp",
		"ＦＵＬＬＷＩＤＴＨ ｆｕｌｌ １２３",
		"Ⅻ roman ⅻ ① circled",
		"CamelCaseIdentifier snake_case_name kebab-case-name",
		"The quick brown fox jumps over the lazy dog's back 1234567890",
	}
	for _, rng := range []int64{1, 2, 3} {
		r := rand.New(rand.NewSource(rng))
		var b strings.Builder
		for i := 0; i < 2000; i++ {
			switch r.Intn(6) {
			case 0:
				b.WriteByte(byte(r.Intn(256)))
			case 1:
				b.WriteRune(rune(r.Intn(0x3000)))
			case 2:
				b.WriteString(" ")
			default:
				b.WriteByte(byte('A' + r.Intn(58)))
			}
		}
		corpus = append(corpus, b.String())
	}
	ts := newTokenStream()
	for _, text := range corpus {
		var got []string
		ts.each(text, func(tok string) { got = append(got, tok) })
		want := Tokenize(text)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") || len(got) != len(want) {
			t.Errorf("stream tokens differ for %q:\n got  %q\n want %q", text, got, want)
		}
	}
	// Fields tokenized one by one equal the old " "-joined concatenation.
	a := &model.WikiArticle{Title: "Auth\xc3", Summary: "\xa9Middleware", Content: "x",
		Concepts: []string{"a-b", "c"}, Categories: []string{"Ünï", ""}}
	var got []string
	for _, f := range append([]string{a.Title, a.Summary, a.Content}, append(a.Concepts, a.Categories...)...) {
		ts.each(f, func(tok string) { got = append(got, tok) })
	}
	want := Tokenize(a.Title + " " + a.Summary + " " + a.Content + " " +
		strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("per-field tokens %q != joined %q", got, want)
	}
}

// parityCorpus is generateCorpus plus glossary articles (term, aliases,
// multi-word term, alias-only), an empty article and varied categories.
func parityCorpus(n int, seed int64) []*model.WikiArticle {
	arts := generateCorpus(n)
	r := rand.New(rand.NewSource(seed))
	for i, a := range arts {
		a.Content = strings.Repeat(a.Content+"\n\n", 1+r.Intn(4))
		if i%3 == 0 {
			a.Categories = append(a.Categories, "shared")
		}
		if i%7 == 0 {
			a.Categories = nil
		}
	}
	return append(arts,
		&model.WikiArticle{ID: "zz-gloss-cache", Title: "Cache", Summary: "Glossary: Cache",
			Content: "A cache keeps hot data.", Concepts: []string{"glossary"}, Kind: "glossary",
			Term: "Cache", Aliases: []string{"Caching", "queues", ""}, Categories: []string{"glossary"}},
		&model.WikiArticle{ID: "zz-gloss-routing", Title: "Routing Table", Content: "Where routes live.",
			Kind: "glossary", Term: "Routing Table", Aliases: []string{"router"}, Categories: []string{"glossary"}},
		&model.WikiArticle{ID: "zz-gloss-alias", Title: "Handler", Content: "Nothing relevant here.",
			Kind: "glossary", Term: "", Aliases: []string{"handlers", "Middleware"}},
		&model.WikiArticle{ID: "zz-empty", Title: "", Content: "", Categories: []string{"empty"}},
	)
}

func parityQueries(seed int64) []string {
	qs := []string{"", "!!!", "   ", "zzzunknownterm", "42", "the", "a an is",
		"cache", "caching", "Cache", "CACHING queues", "router", "routing table", "handlers",
		"middleware middleware routing", "async database", "encrypted storage handler",
		"processes manages creates", "shared", "glossary", "authentication service handler middleware",
		"database database database", "naïve café", "deletes the struct"}
	vocab := []string{"authentication", "database", "routing", "middleware", "config", "logging",
		"cache", "queue", "storage", "api", "service", "handler", "async", "distributed",
		"stateless", "streaming", "function", "returns", "handles", "processes", "the", "for"}
	r := rand.New(rand.NewSource(seed))
	for i := 0; i < 60; i++ {
		k := 1 + r.Intn(4)
		w := make([]string, k)
		for j := range w {
			w[j] = vocab[r.Intn(len(vocab))]
		}
		qs = append(qs, strings.Join(w, " "))
	}
	return qs
}

func seedScope(t testing.TB, scope string, arts []*model.WikiArticle) []*model.WikiArticle {
	t.Helper()
	os.RemoveAll(store.ScopeDir(scope))
	for _, a := range arts {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatalf("save %s: %v", a.ID, err)
		}
	}
	all, err := store.ListArticles(scope)
	if err != nil {
		t.Fatal(err)
	}
	return all
}

func sameScore(a, b float64) bool {
	return a == b || (math.IsNaN(a) && math.IsNaN(b))
}

// checkParity compares the index-only path with the v3 pipeline for one
// query and exclude set across every limit (the ranking is limit-independent,
// so a limit only cuts a prefix: limit <= 0 still yields one hit). end2end
// also runs SearchScope and checks the loaded articles.
func checkParity(t *testing.T, scope string, ls *legacyScope, si *Index, query, exclude string, end2end bool) {
	t.Helper()
	for _, limit := range []int{1000, 10, 3, 1, 0, -1} {
		wantIDs, wantScores := ls.search(query, limit, exclude)
		ranked := scoreIndex(si, query, limit, exclude)
		if len(ranked) != len(wantIDs) {
			t.Fatalf("q=%q limit=%d exclude=%q: got %d hits, want %d (%v)", query, limit, exclude, len(ranked), len(wantIDs), wantIDs)
		}
		for i, r := range ranked {
			if si.DocIDs[r.doc] != wantIDs[i] || !sameScore(r.score, wantScores[i]) {
				t.Fatalf("q=%q limit=%d exclude=%q rank %d: got %s %v, want %s %v",
					query, limit, exclude, i, si.DocIDs[r.doc], r.score, wantIDs[i], wantScores[i])
			}
		}
		if !end2end || limit != 10 {
			continue
		}
		hits, _ := SearchScope(scope, query, limit, exclude, nil)
		if len(hits) != len(wantIDs) {
			t.Fatalf("SearchScope q=%q: %d hits, want %d", query, len(hits), len(wantIDs))
		}
		for i, h := range hits {
			if h.ID != wantIDs[i] || h.Content != ls.all[indexOfID(ls.all, h.ID)].Content {
				t.Fatalf("SearchScope q=%q rank %d: got %s", query, i, h.ID)
			}
		}
	}
}

// checkScope proves the scope's on-disk index fresh (healing it if needed)
// and checks one query against the v3 pipeline end to end.
func checkScope(t *testing.T, scope, query, exclude string) {
	t.Helper()
	all, _ := store.ListArticles(scope)
	si, _ := FreshIndex(scope, nil)
	if si == nil {
		t.Fatalf("no index for %s", scope)
	}
	checkParity(t, scope, newLegacyScope(all), si, query, exclude, true)
}

func indexOfID(all []*model.WikiArticle, id string) int {
	for i, a := range all {
		if a.ID == id {
			return i
		}
	}
	return -1
}

func TestSearchScopeParityWithV3Pipeline(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	for _, n := range []int{1, 9, 80, 300} {
		scope := fmt.Sprintf("parity-%d", n)
		all := seedScope(t, scope, parityCorpus(n, int64(n)))
		backdate(t, scope, time.Hour)
		ls := newLegacyScope(all)
		si, _ := FreshIndex(scope, nil)
		disk := LoadIndex(scope) // the decoded .bin, lazily decoded postings
		for _, exclude := range []string{"", "database", "cache, api", " logging ,routing", "glossary",
			"shared", "nonexistent", ",", "empty"} {
			for qi, q := range parityQueries(int64(n)) {
				checkParity(t, scope, ls, si, q, exclude, qi%10 == 0)
				checkParity(t, scope, ls, disk, q, exclude, false)
			}
		}
	}
}

// Every kept doc is empty: the v3 slow path scored them NaN and still
// returned them; the masked path reproduces that.
func TestSearchScopeParityAllEmptyAfterExclude(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "parity-empty"
	seedScope(t, scope, []*model.WikiArticle{
		{ID: "e1", Title: ""},
		{ID: "e2", Title: ""},
		{ID: "full", Title: "Full doc", Content: "cache me", Categories: []string{"drop"}},
	})
	for _, q := range []string{"cache", "nothing", "full doc"} {
		checkScope(t, scope, q, "drop")
		checkScope(t, scope, q, "")
	}
	if hits, _ := SearchScope(scope, "cache", 5, "drop", nil); len(hits) != 2 {
		t.Fatalf("NaN-scored empty docs: got %d hits, want the 2 empty docs as in v3", len(hits))
	}
}

func TestV3JSONIndexHealsToBinary(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "heal-v3"
	all := seedScope(t, scope, parityCorpus(20, 3))
	jsonPath := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	v3 := `{"v":3,"doc_ids":["x"],"doc_lens":[1],"avg_dl":1,"postings":{"cach":[[0,1]]},"title_tokens":[[]],"concept_tokens":[[]]}`
	if err := os.WriteFile(jsonPath, []byte(v3), 0o644); err != nil {
		t.Fatal(err)
	}
	if LoadIndex(scope) != nil {
		t.Fatal("a v3 JSON file must not load as an index")
	}
	checkScope(t, scope, "cache handler", "")
	si := LoadIndex(scope)
	if si == nil || si.V != IndexVersion || !IndexMatches(si, all) {
		t.Fatalf("search did not heal to a current .bin matching the scope")
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("the v3 search_index.json was left behind")
	}
}

// TestV4IndexRejectedAndHealed: a v4 .bin stores full-Porter terms
// ("gener" for "generating"), which step-1 query tokens never hit. Its doc
// table would still pass the freshness check, so only the version keeps it
// from mis-scoring: it must not load, and a search must rewrite it.
func TestV4IndexRejectedAndHealed(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "heal-v4"
	all := seedScope(t, scope, append(parityCorpus(10, 4), &model.WikiArticle{
		ID: "zz-gen", Title: "Generating reports", Content: "Generating a report takes a minute.", Version: 1,
	}))
	if err := SaveIndex(scope, BuildIndex(all)); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(IndexPath(scope))
	if err != nil {
		t.Fatal(err)
	}
	// The header is outside the CRC, so this is a well-formed v4 file.
	binary.LittleEndian.PutUint32(data[4:], 4)
	if err := os.WriteFile(IndexPath(scope), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if si := LoadIndex(scope); si != nil {
		t.Fatalf("a v4 index loaded (v=%d); its terms predate the current stemmer", si.V)
	}
	checkScope(t, scope, "generating report", "")
	si := LoadIndex(scope)
	if si == nil || si.V != IndexVersion || si.V == 4 || !IndexMatches(si, all) {
		t.Fatalf("search did not heal the v4 index to v%d", IndexVersion)
	}
	if len(si.postingsFor("generate")) == 0 {
		t.Fatalf("healed index has no postings for the step-1 stem %q", "generate")
	}
}

func TestCorruptIndexFallsBackToRebuild(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "corrupt"
	all := seedScope(t, scope, parityCorpus(30, 5))
	if err := SaveIndex(scope, BuildIndex(all)); err != nil {
		t.Fatal(err)
	}
	good, err := os.ReadFile(IndexPath(scope))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string][]byte{
		"empty":       {},
		"magic":       append([]byte("XXXX"), good[4:]...),
		"version":     func() []byte { b := clone(good); binary.LittleEndian.PutUint32(b[4:], 3); return b }(),
		"crc":         func() []byte { b := clone(good); b[len(b)-1] ^= 0xff; return b }(),
		"truncated":   good[:len(good)/2],
		"header-only": good[:indexHeaderLen],
		"extra":       append(clone(good), 0),
		"json":        []byte(`{"v":4}`),
	}
	for name, data := range cases {
		if err := os.WriteFile(IndexPath(scope), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if LoadIndex(scope) != nil {
			t.Fatalf("%s: corrupt index loaded", name)
		}
		checkScope(t, scope, "cache middleware", "")
		if si := LoadIndex(scope); si == nil || !IndexMatches(si, all) {
			t.Fatalf("%s: search did not rewrite a valid index", name)
		}
	}
	// Every truncation and every byte flip is rejected without a panic.
	for l := 0; l < len(good); l++ {
		if si, err := decodeIndex(good[:l]); err == nil || si != nil {
			t.Fatalf("truncation to %d bytes decoded", l)
		}
	}
	// Flips behind a recomputed CRC reach the structural checks: they may
	// decode, but decoding and postings lookups must never panic.
	terms := []string{"cach", "middlewar", "handler", "zz", ""}
	for i := indexHeaderLen; i < len(good); i++ {
		b := clone(good)
		b[i] ^= 0x5a
		binary.LittleEndian.PutUint32(b[8:], crc32.Checksum(b[indexHeaderLen:], crcTable))
		si, err := decodeIndex(b)
		if err != nil {
			continue
		}
		for _, term := range terms {
			for _, p := range si.postingsFor(term) {
				if p[0] < 0 || p[0] >= len(si.DocIDs) {
					t.Fatalf("flip at %d: posting doc %d out of range", i, p[0])
				}
			}
		}
		scoreIndex(si, "cache middleware handler", 5, "shared")
	}
}

func clone(b []byte) []byte { return append([]byte(nil), b...) }

func backdate(t *testing.T, scope string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	entries, _ := os.ReadDir(wikiDir(scope))
	for _, e := range entries {
		os.Chtimes(filepath.Join(wikiDir(scope), e.Name()), old, old)
	}
}

func TestStaleDocTableDetected(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "stale"
	seedScope(t, scope, parityCorpus(12, 9))
	backdate(t, scope, time.Hour)
	fresh := func() bool {
		si := LoadIndex(scope)
		if si == nil {
			return false
		}
		ok, _ := si.checkFresh(scope)
		return ok
	}
	heal := func() {
		t.Helper()
		if si, _ := FreshIndex(scope, nil); si == nil {
			t.Fatal("no index")
		}
		if !fresh() {
			t.Fatal("index not fresh right after a heal")
		}
	}
	heal()

	// Added file.
	store.SaveArticle(scope, &model.WikiArticle{ID: "aaa-new", Title: "New", Content: "brand new otter"})
	if fresh() {
		t.Fatal("added file not detected")
	}
	heal()
	checkScope(t, scope, "otter", "")

	// Removed file.
	os.Remove(filepath.Join(wikiDir(scope), "aaa-new.md"))
	if fresh() {
		t.Fatal("removed file not detected")
	}
	heal()

	// Rewritten under the same id with a different size.
	all, _ := store.ListArticles(scope)
	victim := all[3]
	victim.Content = "rewritten otter otter body"
	store.SaveArticle(scope, victim)
	if fresh() {
		t.Fatal("same-id rewrite not detected")
	}
	heal()
	checkScope(t, scope, "otter", "")
}

// A rewrite that keeps both mtime and size (same clock tick) is caught by the
// racy-stamp hash, and a settled racy stamp is dropped from the file.
func TestStaleSameMtimeSameSizeRewrite(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "racy"
	seedScope(t, scope, []*model.WikiArticle{
		{ID: "a", Title: "A", Content: "alpha otter"},
		{ID: "b", Title: "B", Content: "bravo bison"},
	})
	path := filepath.Join(wikiDir(scope), "b.md")
	info, _ := os.Stat(path)
	mtime := info.ModTime()
	// Freshly written: stamps are racy, so they carry hashes.
	si, _ := FreshIndex(scope, nil)
	if si.stamps[1].hash == "" {
		t.Fatal("a just-written file must get a racy hash")
	}
	data, _ := os.ReadFile(path)
	swapped := strings.Replace(string(data), "bravo bison", "bravo otter", 1)
	if len(swapped) != len(data) {
		t.Fatal("test rewrite must keep the size")
	}
	os.WriteFile(path, []byte(swapped), 0o644)
	os.Chtimes(path, mtime, mtime)
	if ok, _ := LoadIndex(scope).checkFresh(scope); ok {
		t.Fatal("same-mtime same-size rewrite not detected")
	}
	checkScope(t, scope, "otter", "")

	// Once the files are older than the racy window, a fresh check settles
	// the stamps and rewrites the index without hashes.
	backdate(t, scope, time.Hour)
	FreshIndex(scope, nil) // mtimes changed: rebuild, stamps now non-racy
	si = LoadIndex(scope)
	for i, st := range si.stamps {
		if st.hash != "" {
			t.Fatalf("doc %d: settled file still carries a hash", i)
		}
	}
}

func TestRacyStampsSettle(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "settle"
	seedScope(t, scope, parityCorpus(5, 1))
	if si, _ := FreshIndex(scope, nil); si.stamps[0].hash == "" {
		t.Fatal("expected racy stamps right after writing")
	}
	// Files now an hour old, index stamps still racy (hash set): the next
	// check verifies the hashes, finds them settled, and rewrites the index.
	backdate(t, scope, time.Hour)
	FreshIndex(scope, nil) // mtimes moved: rebuild with non-racy stamps
	si := LoadIndex(scope)
	for i, id := range si.DocIDs {
		h, _ := hashFile(filepath.Join(wikiDir(scope), id+".md"))
		si.stamps[i].hash = h
	}
	if err := SaveIndex(scope, si); err != nil {
		t.Fatal(err)
	}
	got, listed := FreshIndex(scope, nil)
	if listed != nil {
		t.Fatal("unchanged files with racy stamps must verify by hash, not rebuild")
	}
	for i, st := range got.stamps {
		if st.hash != "" {
			t.Fatalf("doc %d not settled", i)
		}
	}
	if disk := LoadIndex(scope); disk == nil || disk.stamps[0].hash != "" {
		t.Fatal("settled stamps were not persisted")
	}
}

// An unparseable wiki file is skipped by ListArticles; the index records it
// so it does not force a rebuild on every search, and editing it does.
func TestUnparseableFileIsStampedNotRebuilt(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "unparseable"
	seedScope(t, scope, parityCorpus(6, 2))
	bad := filepath.Join(wikiDir(scope), "broken.md")
	os.WriteFile(bad, []byte("---\n{not json\n---\nbody"), 0o644)
	backdate(t, scope, time.Hour)
	if _, listed := FreshIndex(scope, nil); listed == nil {
		t.Fatal("first search should rebuild")
	}
	if _, listed := FreshIndex(scope, nil); listed != nil {
		t.Fatal("an unparseable file forced a second rebuild")
	}
	checkScope(t, scope, "cache", "")
	os.WriteFile(bad, []byte("---\n{\"title\":\"Fixed otter\"}\n---\nbody"), 0o644)
	if _, listed := FreshIndex(scope, nil); listed == nil {
		t.Fatal("fixing the broken file did not rebuild")
	}
	checkScope(t, scope, "otter", "")
}

// Writers save the index right after writing articles (no pre-stamp): a file
// the writer's listing missed but that parses is never recorded as ignored.
func TestWriterSaveLeavesUnindexedArticleStale(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "writer"
	all := seedScope(t, scope, parityCorpus(4, 4))
	store.SaveArticle(scope, &model.WikiArticle{ID: "late", Title: "Late otter"})
	if err := SaveIndex(scope, BuildIndex(all)); err != nil {
		t.Fatal(err)
	}
	if ok, _ := LoadIndex(scope).checkFresh(scope); ok {
		t.Fatal("index missing a parseable article was considered fresh")
	}
	checkScope(t, scope, "otter", "")
}

// Searching a scope that does not exist, or has no articles, writes nothing.
func TestSearchScopeEmptyWritesNothing(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	if hits, si := SearchScope("no-such-scope", "x", 5, "", nil); hits != nil || si != nil {
		t.Fatal("search of a missing scope returned something")
	}
	if _, err := os.Stat(store.ScopeDir("no-such-scope")); !os.IsNotExist(err) {
		t.Fatal("search created a scope dir")
	}
	store.EnsureDirs("empty-scope")
	SearchScope("empty-scope", "x", 5, "", nil)
	if _, err := os.Stat(IndexPath("empty-scope")); !os.IsNotExist(err) {
		t.Fatal("search of an empty scope wrote an index")
	}
}
