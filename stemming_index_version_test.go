// stemming_index_version_test.go — Pins the search-index version hazard that
// stemming introduces. A v2 search_index.json (written before tokenize()
// Porter-stemmed) stores RAW terms in its postings; a stemmed query token like
// "open" would silently miss a doc stored as "opens" if that index were trusted.
// The index format is therefore v3 (stemmed postings): a v2 file must be
// ignored on load, search must fall back to on-the-fly tokenization, and the
// next full-scope search must heal the file to v3.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
)

// rawTokenize is the pre-stemming tokenizer: lowercase + split on
// non-alphanumerics, no stemming. It reproduces what a v2 index stored.
func rawTokenize(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	})
}

// writeUnstemmedV2Index persists an index exactly as pre-stemming kb wrote it:
// {"v":2, ...} with raw, unstemmed postings.
func writeUnstemmedV2Index(t *testing.T, scope string, articles []*model.WikiArticle) string {
	t.Helper()
	si := &SearchIndex{
		V:             2,
		DocIDs:        make([]string, len(articles)),
		DocLens:       make([]int, len(articles)),
		Postings:      map[string][]Posting{},
		TitleTokens:   make([][]string, len(articles)),
		ConceptTokens: make([][]string, len(articles)),
	}
	total := 0
	for i, a := range articles {
		all := rawTokenize(a.Title + " " + a.Summary + " " + a.Content +
			" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
		si.DocIDs[i] = a.ID
		si.DocLens[i] = len(all)
		si.TitleTokens[i] = rawTokenize(a.Title)
		si.ConceptTokens[i] = rawTokenize(strings.Join(a.Concepts, " "))
		total += len(all)
		tfs := map[string]int{}
		for _, tok := range all {
			tfs[tok]++
		}
		for term, tf := range tfs {
			si.Postings[term] = append(si.Postings[term], Posting{i, tf})
		}
	}
	si.AvgDL = float64(total) / float64(len(articles))
	data, err := json.Marshal(si)
	if err != nil {
		t.Fatalf("marshal v2 index: %v", err)
	}
	path := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write v2 index: %v", err)
	}
	return path
}

func TestUnstemmedV2IndexIsNotTrusted(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "sidx-stem-" + filepath.Base(dir)
	ensureDirs(scope)

	for _, a := range []*model.WikiArticle{
		{ID: "hours", Title: "Store Hours", Content: "The shop opens at 8am and closes at 6pm.", Version: 1},
		{ID: "payments", Title: "Payments", Content: "We accept cards and cash for all purchases.", Version: 1},
	} {
		if err := saveArticle(scope, a); err != nil {
			t.Fatalf("saveArticle %s: %v", a.ID, err)
		}
	}
	articles, err := listArticles(scope)
	if err != nil {
		t.Fatalf("listArticles: %v", err)
	}
	writeUnstemmedV2Index(t, scope, articles)

	// The pre-stemming v2 file must not load: its raw postings can't serve
	// stemmed query tokens.
	if si := loadSearchIndex(scope); si != nil {
		t.Errorf("loadSearchIndex trusted a pre-stemming v2 index (v=%d)", si.V)
	}

	// Search through the same load the CLI uses: "open" must find "opens".
	if hits := bm25SearchWithIndex(articles, "open", 5, loadSearchIndex(scope)); !resultHasID(hits, "hours") {
		t.Errorf("query 'open' missed the 'opens' doc against a v2 index on disk; got %v", idsOf(hits))
	}

	// MCP / CLI heal path: returns a usable stemmed index and rewrites the file.
	si := loadOrHealSearchIndex(scope, articles)
	if si == nil || si.V != searchIndexVersion {
		t.Fatalf("loadOrHealSearchIndex did not rebuild the stale index: %+v", si)
	}
	if hits := bm25SearchWithIndex(articles, "open", 5, si); !resultHasID(hits, "hours") {
		t.Errorf("healed index: query 'open' missed the 'opens' doc; got %v", idsOf(hits))
	}
	if disk := loadSearchIndex(scope); disk == nil || !indexMatches(disk, articles) {
		t.Fatalf("heal did not persist a current-version index")
	}

	// Full CLI path also heals a freshly re-planted v2 file.
	writeUnstemmedV2Index(t, scope, articles)
	cmdSearch([]string{"open", "--scope", scope, "--json"})
	if disk := loadSearchIndex(scope); disk == nil || disk.V != searchIndexVersion {
		t.Fatalf("cmdSearch did not heal the v2 index to v%d", searchIndexVersion)
	}
}
