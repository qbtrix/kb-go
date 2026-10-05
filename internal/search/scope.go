// Single-scope BM25 search straight from the persisted index: one ReadDir
// proves the index fresh (fresh.go), scoring reads only the query terms'
// postings plus the per-doc metadata in the doc table, and only the ranked
// top-k articles are read from disk. This is the `kb search` / MCP kb_search
// path for one scope; multi-scope searches still list every article.
//
// Invariant: results equal BM25WithIndex over store.ListArticles, ids AND
// scores — full-scope searches use the postings arithmetic, --exclude-tags
// searches the slow-path arithmetic over the filtered subset (its N, df and
// avgdl), and both rank through rankPositions over a slice laid out like the
// old article slice, so ties break identically.

package search

import (
	"math"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
)

// scoredDoc is one ranked hit: a doc index into the index and its score.
type scoredDoc struct {
	doc   int
	score float64
}

// FreshIndex returns an index that describes the scope's wiki/ exactly as it
// is on disk, trying cached (may be nil), then the persisted file, then a
// rebuild from store.ListArticles that is persisted best-effort. listed is the
// full article slice when it had to rebuild (aligned with the index), else
// nil. Returns nil when the scope has no wiki/ or no parseable article — and
// then writes nothing, so a search never creates a scope.
func FreshIndex(scope string, cached *Index) (si *Index, listed []*model.WikiArticle) {
	dir := wikiDir(scope)
	now := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	hasMarkdown := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			hasMarkdown = true
			break
		}
	}
	if !hasMarkdown {
		return nil, nil
	}
	candidates := []func() *Index{
		func() *Index { return cached },
		func() *Index { return LoadIndex(scope) },
	}
	for _, load := range candidates {
		cand := load()
		if cand == nil {
			continue
		}
		if fresh, settle := cand.freshEntries(dir, entries, now); fresh {
			if settle {
				cand.settleStamps(now)
				if data, err := encodeIndex(cand); err == nil {
					_ = writeFileAtomic(IndexPath(scope), data)
				}
			}
			return cand, nil
		}
	}

	// Stamp first, then read: a write in between leaves the index looking
	// stale (rebuilt next time), never fresh with old content.
	snap := snapshotWiki(dir, entries, now)
	listed, err = store.ListArticles(scope)
	if err != nil || len(listed) == 0 {
		return nil, nil
	}
	si = BuildIndex(listed)
	si.stamps, si.ignored = assignStamps(snap, si.DocIDs)
	_ = SaveIndex(scope, si)
	return si, listed
}

// SearchScope is `kb search` over one scope: BM25 from a fresh index (cached
// may be nil), --exclude-tags applied from the doc table (excludeTags is the
// raw comma-separated flag value, "" for none), and only the top hits loaded.
// It returns the index it used so a long-lived caller can pass it back in.
func SearchScope(scope, query string, limit int, excludeTags string, cached *Index) ([]*model.WikiArticle, *Index) {
	si, listed := FreshIndex(scope, cached)
	if si == nil {
		return nil, nil
	}
	return loadHits(scope, si, listed, scoreIndex(si, query, limit, excludeTags)), si
}

// loadHits materializes ranked docs: from listed when the caller already holds
// the scope's articles, else one store.LoadArticle per hit. A hit whose file
// vanished or no longer parses since the freshness check is skipped.
func loadHits(scope string, si *Index, listed []*model.WikiArticle, ranked []scoredDoc) []*model.WikiArticle {
	var out []*model.WikiArticle
	for _, r := range ranked {
		if listed != nil {
			out = append(out, listed[r.doc])
			continue
		}
		a, err := store.LoadArticle(scope, si.DocIDs[r.doc])
		if err != nil || a == nil {
			continue
		}
		out = append(out, a)
	}
	return out
}

// scoreIndex ranks the index's docs for query, mirroring BM25WithIndex over
// the full article slice (excludeTags == "") or over the tag-filtered slice.
func scoreIndex(si *Index, query string, limit int, excludeTags string) []scoredDoc {
	n := len(si.DocIDs)
	if n == 0 || query == "" {
		return nil
	}
	queryTerms := Tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	var docs []int // position -> doc index; nil means identity
	var scores []float64
	if excludeTags == "" {
		scores = bm25ScoresFromPostings(queryTerms, si)
	} else {
		docs = keptDocs(si, excludeTags)
		if len(docs) == 0 {
			return nil
		}
		scores = bm25ScoresMasked(si, docs, queryTerms)
	}
	glossaryBoostIndex(si, docs, queryTerms, scores)

	positions := rankPositions(scores, limit)
	out := make([]scoredDoc, len(positions))
	for i, p := range positions {
		d := p
		if docs != nil {
			d = docs[p]
		}
		out[i] = scoredDoc{d, scores[p]}
	}
	return out
}

// keptDocs lists, in index order, the docs whose categories contain none of
// the comma-separated tags (each trimmed), as cmdSearch's filter did.
func keptDocs(si *Index, excludeTags string) []int {
	excluded := strings.Split(excludeTags, ",")
	for i := range excluded {
		excluded[i] = strings.TrimSpace(excluded[i])
	}
	kept := make([]int, 0, len(si.DocIDs))
	for d := range si.DocIDs {
		skip := false
		for _, tag := range excluded {
			if slices.Contains(si.categories[d], tag) {
				skip = true
				break
			}
		}
		if !skip {
			kept = append(kept, d)
		}
	}
	return kept
}

// bm25ScoresMasked is bm25ScoresSlow over the kept docs, computed from
// postings: N, df and avgdl count kept docs only, and each doc's score is
// accumulated term by term in query order (base, then the title and concept
// boosts) exactly as the slow path's running sum, so the floats match. A term
// missing from a doc adds +0 there in the slow path, which changes nothing.
func bm25ScoresMasked(si *Index, docs []int, queryTerms []string) []float64 {
	pos := make([]int32, len(si.DocIDs))
	for i := range pos {
		pos[i] = -1
	}
	totalLen := 0
	for k, d := range docs {
		pos[d] = int32(k)
		totalLen += si.DocLens[d]
	}
	nDocs := float64(len(docs))
	avgDL := float64(totalLen) / nDocs
	scores := make([]float64, len(docs))
	if totalLen == 0 {
		// Every kept doc is empty: the slow path's dl/avgDL is 0/0, so every
		// score is NaN. Reproduce it rather than silently differ.
		for k := range scores {
			scores[k] = math.NaN()
		}
		return scores
	}
	for _, term := range queryTerms {
		plist := si.postingsFor(term)
		df := 0
		for _, p := range plist {
			if pos[p[0]] >= 0 {
				df++
			}
		}
		idf := math.Log((nDocs-float64(df)+0.5)/(float64(df)+0.5) + 1)
		for _, p := range plist {
			k := pos[p[0]]
			if k < 0 {
				continue
			}
			tf := float64(p[1])
			dl := float64(si.DocLens[p[0]])
			num := tf * (BM25K1 + 1)
			den := tf + BM25K1*(1-BM25B+BM25B*dl/avgDL)
			base := idf * num / den
			scores[k] += base
			if slices.Contains(si.TitleTokens[p[0]], term) {
				scores[k] += base * 2.0
			}
			if slices.Contains(si.ConceptTokens[p[0]], term) {
				scores[k] += base * 1.0
			}
		}
	}
	return scores
}

// glossaryBoostIndex is applyGlossaryBoost reading the glossary keys stored in
// the index (already lowercased and stemmed at build time).
func glossaryBoostIndex(si *Index, docs []int, queryTerms []string, scores []float64) {
	for k := range scores {
		d := k
		if docs != nil {
			d = docs[k]
		}
		if !si.glossary[d] {
			continue
		}
		matched := false
		for _, qt := range queryTerms {
			qLower := strings.ToLower(qt)
			if si.termKeys[d] != "" && si.termKeys[d] == qLower {
				matched = true
				break
			}
			if slices.Contains(si.aliasKeys[d], qLower) {
				matched = true
				break
			}
		}
		if matched {
			scores[k] = (scores[k] + 1.0) * glossaryExactBoost
		}
	}
}
