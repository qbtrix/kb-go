// The persisted inverted search index (term -> postings, per-doc lengths):
// build, save, load, version checks and self-healing rebuilds.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
)

// searchIndexVersion is bumped whenever the on-disk shape OR the tokens it
// stores change. v1 was a token dump ({"articles": [...]}, unmarshals as V==0);
// v2 was the inverted index with raw, unstemmed terms; v3 is the same shape
// with Porter-stemmed terms (tokenize). Any other version is ignored on load —
// a v2 file's raw postings would silently miss stemmed query tokens — so
// search falls back to on-the-fly tokenization and the next index write (or a
// full-scope search, via loadOrHealSearchIndex) replaces the file.
const searchIndexVersion = 3

// Posting is one (docIdx, termFrequency) pair in a term's postings list.
// Encoded as a 2-element JSON array to keep the index file compact.
type Posting [2]int

// SearchIndex is an inverted index over the articles of a scope. BM25 scoring
// reads ONLY the postings lists of the query terms — document frequency is
// the postings length, term frequency is stored per entry — instead of
// scanning every document's full token slice. Per-doc title/concept token
// sets are kept for the title/concept boosts; the glossary boost reads
// Kind/Term/Aliases from the articles themselves, so nothing else is needed.
type SearchIndex struct {
	V             int                  `json:"v"`
	DocIDs        []string             `json:"doc_ids"`
	DocLens       []int                `json:"doc_lens"`
	AvgDL         float64              `json:"avg_dl"`
	Postings      map[string][]Posting `json:"postings"`
	TitleTokens   [][]string           `json:"title_tokens"`
	ConceptTokens [][]string           `json:"concept_tokens"`
}

func buildSearchIndex(articles []*model.WikiArticle) *SearchIndex {
	n := len(articles)
	si := &SearchIndex{
		V:             searchIndexVersion,
		DocIDs:        make([]string, n),
		DocLens:       make([]int, n),
		Postings:      make(map[string][]Posting),
		TitleTokens:   make([][]string, n),
		ConceptTokens: make([][]string, n),
	}
	totalLen := 0
	for i, a := range articles {
		all := tokenize(a.Title + " " + a.Summary + " " + a.Content +
			" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
		si.DocIDs[i] = a.ID
		si.DocLens[i] = len(all)
		si.TitleTokens[i] = tokenize(a.Title)
		si.ConceptTokens[i] = tokenize(strings.Join(a.Concepts, " "))
		totalLen += len(all)

		tfs := make(map[string]int)
		for _, tok := range all {
			tfs[tok]++
		}
		for term, tf := range tfs {
			si.Postings[term] = append(si.Postings[term], Posting{i, tf})
		}
	}
	// Postings lists are appended in doc order per term; sort for a
	// deterministic file (map iteration order above is per-doc, so entries
	// are already in doc order — this is belt and braces for future writers).
	for _, plist := range si.Postings {
		sort.Slice(plist, func(a, b int) bool { return plist[a][0] < plist[b][0] })
	}
	if n > 0 {
		si.AvgDL = float64(totalLen) / float64(n)
	}
	return si
}

func saveSearchIndex(scope string, si *SearchIndex) error {
	store.EnsureDirs(scope)
	path := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	data, err := json.Marshal(si)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadSearchIndex(scope string) *SearchIndex {
	path := filepath.Join(store.ScopeDir(scope), "cache", "search_index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var si SearchIndex
	if err := json.Unmarshal(data, &si); err != nil {
		return nil
	}
	if si.V != searchIndexVersion {
		// Old-format (or future-format) index: ignore it. Search runs the
		// on-the-fly slow path; the next index write upgrades the file.
		return nil
	}
	return &si
}

// loadOrHealSearchIndex returns a search index that matches the given
// articles, rebuilding and best-effort persisting it when the on-disk one is
// missing, old-format, or stale. This lets read-only consumers (plain `kb
// search`, MCP serve) regain the fast path after a format upgrade discarded
// their v1 file — without it, a scope that never sees another ingest/build
// would pay the slow path forever. Callers MUST pass the scope's FULL article
// slice in store.ListArticles order (never a tag-filtered or multi-scope slice),
// since the persisted index describes the whole scope.
func loadOrHealSearchIndex(scope string, articles []*model.WikiArticle) *SearchIndex {
	return healSearchIndex(scope, articles, loadSearchIndex(scope))
}

// healSearchIndex is loadOrHealSearchIndex with the on-disk index already
// loaded (si may be nil); the MCP server passes its cached copy here.
func healSearchIndex(scope string, articles []*model.WikiArticle, si *SearchIndex) *SearchIndex {
	if indexMatches(si, articles) {
		return si
	}
	if len(articles) == 0 {
		// Nothing to index — and healing here would create scope dirs on a
		// pure read (e.g. searching a scope that doesn't exist).
		return nil
	}
	si = buildSearchIndex(articles)
	// Best-effort: the index is a cache. A failed write (read-only FS,
	// permissions) must not fail the search — scoring proceeds from the
	// freshly built in-memory index either way.
	_ = saveSearchIndex(scope, si)
	return si
}

// indexMatches reports whether si describes exactly the given article slice
// (same length, same ids, same order). A stale index — e.g. articles were
// added or removed without a rebuild — must not be trusted for scoring.
func indexMatches(si *SearchIndex, articles []*model.WikiArticle) bool {
	if si == nil || si.V != searchIndexVersion || len(si.DocIDs) != len(articles) {
		return false
	}
	for i, a := range articles {
		if si.DocIDs[i] != a.ID {
			return false
		}
	}
	return true
}
