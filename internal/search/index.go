// Package search is kb's retrieval layer over a scope's wiki articles:
// Porter-stemmed tokenization, BM25 ranking with title (3x), concept (2x) and
// glossary exact-Term/Alias (10x) boosts, the persisted inverted index
// (index.go, binary codec in indexfile.go, wiki freshness in fresh.go), the
// single-scope search path that ranks from the index alone and loads only the
// top-k articles (scope.go), `kb search --context` excerpts (context.go), and
// vector and hybrid (RRF) search over the per-scope vector index (vector.go).
//
// Invariants:
//   - The index stores stemmed postings under IndexVersion. Changing Tokenize
//     means bumping IndexVersion, so an index written with the old tokens is
//     ignored and rebuilt instead of mis-scoring.
//   - Doc order in the index is store.ListArticles order (ascending id):
//     ranking ties break by that order, so the single-scope path only trusts
//     an index whose DocIDs are strictly ascending.
//   - Scores are float-for-float those of BM25WithIndex: full-scope searches
//     use the postings arithmetic, tag-filtered ones the slow-path arithmetic
//     over the filtered subset, exactly as before the index went binary.
package search

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
)

// IndexVersion is bumped whenever the on-disk shape OR the tokens it stores
// change. v1 was a JSON token dump, v2 a JSON inverted index with raw terms,
// v3 the same JSON with Porter-stemmed terms (cache/search_index.json), v4 the
// binary cache/search_index.bin (indexfile.go) that also carries the per-doc
// metadata search needs (glossary keys, categories, file stamps). Older files
// are never read; the next index write or search replaces them.
const IndexVersion = 4

const (
	indexFileName       = "search_index.bin"
	legacyIndexFileName = "search_index.json" // v1-v3; removed when a .bin is written
)

// IndexPath is where a scope's search index lives.
func IndexPath(scope string) string {
	return filepath.Join(store.ScopeDir(scope), "cache", indexFileName)
}

// Posting is one (docIdx, termFrequency) pair in a term's postings list.
type Posting [2]int

// Index is an inverted index over the articles of a scope. BM25 scoring reads
// ONLY the postings of the query terms: df is the postings length, tf is stored
// per entry. Per-doc title/concept token sets drive the title/concept boosts;
// glossary keys and categories let the single-scope path apply the glossary
// boost and --exclude-tags without loading any article.
//
// A built index holds every postings list in Postings; one decoded from disk
// leaves Postings nil and decodes a term's list on demand (postingsFor).
type Index struct {
	V             int
	DocIDs        []string
	DocLens       []int
	AvgDL         float64
	Postings      map[string][]Posting
	TitleTokens   [][]string
	ConceptTokens [][]string

	glossary   []bool     // Kind == "glossary"
	termKeys   []string   // porterStem(lower(Term)), glossary docs only
	aliasKeys  [][]string // porterStem(lower(Alias)), glossary docs only
	categories [][]string // raw categories, for --exclude-tags

	stamps  []docStamp    // wiki file stamp per doc (nil: not taken yet)
	ignored []ignoredFile // wiki/*.md files ListArticles skips (unparseable)

	file *indexFile // set when decoded from disk: lazy postings + raw sections
}

// postingsFor returns term's postings list in doc order (nil when absent).
func (si *Index) postingsFor(term string) []Posting {
	if si.file != nil {
		return si.file.postings(term)
	}
	return si.Postings[term]
}

// BuildIndex indexes articles in the given order (callers persisting it pass
// the scope's full store.ListArticles slice). Tokens come from one streaming
// tokenizer pass per field, identical to Tokenize over the old
// title+summary+content+concepts+categories concatenation.
func BuildIndex(articles []*model.WikiArticle) *Index {
	n := len(articles)
	si := &Index{
		V:             IndexVersion,
		DocIDs:        make([]string, n),
		DocLens:       make([]int, n),
		Postings:      make(map[string][]Posting),
		TitleTokens:   make([][]string, n),
		ConceptTokens: make([][]string, n),
		glossary:      make([]bool, n),
		termKeys:      make([]string, n),
		aliasKeys:     make([][]string, n),
		categories:    make([][]string, n),
	}
	ts := newTokenStream()
	tf := make(map[string]int, 1024)
	dl := 0
	count := func(tok string) { tf[tok]++; dl++ }
	var title, concept []string
	countTitle := func(tok string) { count(tok); title = append(title, tok) }
	countConcept := func(tok string) { count(tok); concept = append(concept, tok) }

	totalLen := 0
	for i, a := range articles {
		clear(tf)
		dl = 0
		title, concept = []string{}, []string{}
		ts.each(a.Title, countTitle)
		ts.each(a.Summary, count)
		ts.each(a.Content, count)
		for _, c := range a.Concepts {
			ts.each(c, countConcept)
		}
		for _, c := range a.Categories {
			ts.each(c, count)
		}
		si.DocIDs[i] = a.ID
		si.DocLens[i] = dl
		si.TitleTokens[i] = title
		si.ConceptTokens[i] = concept
		si.categories[i] = a.Categories
		if a.Kind == "glossary" {
			si.glossary[i] = true
			si.termKeys[i] = porterStem(strings.ToLower(a.Term))
			keys := make([]string, len(a.Aliases))
			for k, al := range a.Aliases {
				keys[k] = porterStem(strings.ToLower(al))
			}
			si.aliasKeys[i] = keys
		}
		totalLen += dl
		// Docs are visited in order, so every list stays sorted by docIdx.
		for term, f := range tf {
			si.Postings[term] = append(si.Postings[term], Posting{i, f})
		}
	}
	if n > 0 {
		si.AvgDL = float64(totalLen) / float64(n)
	}
	return si
}

// SaveIndex writes si as the scope's binary index (atomically, via a temp file
// and rename) and removes a leftover v1-v3 search_index.json, so an older kb
// binary sharing the scope rebuilds its own index instead of trusting a JSON
// file that no longer tracks the wiki. The wiki files are stamped now unless
// si already carries stamps (the heal path stamps BEFORE it reads the files).
func SaveIndex(scope string, si *Index) error {
	store.EnsureDirs(scope)
	if si.stamps == nil {
		si.stamps, si.ignored = stampDocs(scope, si.DocIDs)
	}
	data, err := encodeIndex(si)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(IndexPath(scope), data); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(store.ScopeDir(scope), "cache", legacyIndexFileName))
	return nil
}

// RemoveIndex deletes the scope's search index (binary and legacy JSON).
// A missing file is not an error.
func RemoveIndex(scope string) error {
	dir := filepath.Join(store.ScopeDir(scope), "cache")
	for _, name := range []string{indexFileName, legacyIndexFileName} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// LoadIndex decodes the scope's binary index, or returns nil when it is
// missing, corrupt, truncated or another version. Only the header and doc
// table are decoded here; postings are decoded per query term.
func LoadIndex(scope string) *Index {
	data, err := os.ReadFile(IndexPath(scope))
	if err != nil {
		return nil
	}
	si, err := decodeIndex(data)
	if err != nil {
		return nil
	}
	return si
}

// LoadOrHealIndex returns a search index that matches the given articles AND
// the scope's wiki files on disk, rebuilding and best-effort persisting it
// when the on-disk one is missing, old-format or stale. Callers MUST pass the
// scope's FULL article slice in store.ListArticles order (never a tag-filtered
// or multi-scope slice), since the persisted index describes the whole scope.
func LoadOrHealIndex(scope string, articles []*model.WikiArticle) *Index {
	return HealIndex(scope, articles, LoadIndex(scope))
}

// HealIndex is LoadOrHealIndex with a candidate index already loaded (si may
// be nil).
func HealIndex(scope string, articles []*model.WikiArticle, si *Index) *Index {
	if IndexMatches(si, articles) {
		if fresh, _ := si.checkFresh(scope); fresh {
			return si
		}
	}
	if len(articles) == 0 {
		// Nothing to index — and healing here would create scope dirs on a
		// pure read (e.g. searching a scope that doesn't exist).
		return nil
	}
	si = BuildIndex(articles)
	// Best-effort: the index is a cache. A failed write (read-only FS,
	// permissions) must not fail the search.
	_ = SaveIndex(scope, si)
	return si
}

// IndexMatches reports whether si describes exactly the given article slice
// (same length, same ids, same order). It does not look at file contents; the
// on-disk freshness check is Index.checkFresh.
func IndexMatches(si *Index, articles []*model.WikiArticle) bool {
	if si == nil || si.V != IndexVersion || len(si.DocIDs) != len(articles) {
		return false
	}
	for i, a := range articles {
		if si.DocIDs[i] != a.ID {
			return false
		}
	}
	return true
}
