// Vector and hybrid retrieval over a scope: pure cosine search against the
// per-scope vector index, and hybrid search that fuses BM25 and cosine rankings
// with reciprocal rank fusion (RRF, k=60).
//
// JSON shape contract (search results):
//   - BM25-only: {id, title, summary, concepts}. Vector code adds no keys to it
//     (regression-tested).
//   - Pure cosine: {id, title, summary, concepts, score, vec_rank}.
//   - Hybrid: {id, title, summary, concepts, score, bm25_rank, vec_rank, fused_rank}.
//     `score` carries the fused RRF score; ranks come from the source lists
//     (-1 means the article was not in that list).

package main

import (
	"fmt"
	"sort"

	"github.com/qbtrix/kb-go/internal/model"
)

// rrfK is the standard reciprocal-rank-fusion constant from Cormack et al.
// 2009. Hard-coded — overriding it per-call would invite silent ranking drift
// across consumers and is not in scope.
const rrfK = 60

// rrfFuse merges two ranked lists of article IDs into one fused order.
// Implements reciprocal rank fusion (Cormack et al. 2009): each occurrence
// contributes 1/(k + rank + 1) to the article's score, summed across lists.
// Items present in only one list still get a (smaller) score — the missing
// list simply contributes nothing.
//
// Returns parallel arrays so the caller can build per-result rank metadata
// without re-walking the inputs. fusedScores aligns with fusedIDs by index.
// bm25RankByID and vecRankByID are zero-indexed; -1 means "not in that list".
func rrfFuse(bm25IDs []string, vecIDs []string) (fusedIDs []string, fusedScores []float64, bm25RankByID, vecRankByID map[string]int) {
	bm25RankByID = make(map[string]int)
	vecRankByID = make(map[string]int)
	scores := make(map[string]float64)
	for rank, id := range bm25IDs {
		bm25RankByID[id] = rank
		scores[id] += 1.0 / float64(rrfK+rank+1)
	}
	for rank, id := range vecIDs {
		vecRankByID[id] = rank
		scores[id] += 1.0 / float64(rrfK+rank+1)
	}
	type kv struct {
		id    string
		score float64
	}
	pairs := make([]kv, 0, len(scores))
	for id, s := range scores {
		pairs = append(pairs, kv{id, s})
	}
	// Stable: by score desc, then by id asc to make ties deterministic.
	// Determinism matters for golden-output tests and consumer caching.
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].score != pairs[j].score {
			return pairs[i].score > pairs[j].score
		}
		return pairs[i].id < pairs[j].id
	})
	fusedIDs = make([]string, len(pairs))
	fusedScores = make([]float64, len(pairs))
	for i, p := range pairs {
		fusedIDs[i] = p.id
		fusedScores[i] = p.score
	}
	return fusedIDs, fusedScores, bm25RankByID, vecRankByID
}

// vectorSearchResult bundles a hit's article with the metadata we want to
// surface in JSON output for vector / hybrid modes. The plain WikiArticle has
// no slot for score or rank — they're properties of the query, not the doc.
type vectorSearchResult struct {
	Article   *model.WikiArticle
	Score     float64 // cosine for pure-vec, RRF fused for hybrid
	BM25Rank  int     // -1 when not in BM25 list
	VecRank   int     // -1 when not in vec list
	FusedRank int     // -1 for non-hybrid modes
}

// runVectorSearch performs pure cosine search over the per-scope vector index.
// Used when --query-vec is set without --hybrid.
func runVectorSearch(scope string, queryVec []float32, topK int) ([]vectorSearchResult, error) {
	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		return nil, fmt.Errorf("load vector index: %w", err)
	}
	hits := idx.Search(queryVec, topK)
	out := make([]vectorSearchResult, 0, len(hits))
	for rank, h := range hits {
		a, err := loadArticle(scope, h.ID)
		if err != nil || a == nil {
			// Vector orphan (vector exists for an article that's been deleted).
			// Skip silently — orphans are a maintenance issue, not a query-time error.
			continue
		}
		out = append(out, vectorSearchResult{
			Article:   a,
			Score:     float64(h.Score),
			BM25Rank:  -1,
			VecRank:   rank,
			FusedRank: -1,
		})
	}
	return out, nil
}

// runHybridSearch fuses BM25 over the article corpus with cosine over the
// vector index using reciprocal rank fusion. Both sides run independently
// against the full corpus / index — RRF only re-orders by combined rank, it
// does not re-score with raw values, so the BM25 and cosine numbers don't
// have to be on the same scale.
//
// articlesByID lets us materialize the fused ID order back into article
// pointers without re-listing on each lookup. Articles missing from the
// listing are skipped (orphan vectors, mid-query deletions).
func runHybridSearch(scope string, queryText string, queryVec []float32, topK int) ([]vectorSearchResult, error) {
	// BM25 side — same code path as the existing search.
	allArticles, err := listArticles(scope)
	if err != nil {
		return nil, fmt.Errorf("list articles: %w", err)
	}
	si := loadSearchIndex(scope)
	// For RRF we want a deeper BM25 list than topK so low-vec-ranked items
	// have a chance to surface via fusion. 4*topK is a coarse heuristic; the
	// CLI doesn't expose a fusion-depth flag yet (see future-upgrades).
	bm25Depth := topK * 4
	if bm25Depth < 20 {
		bm25Depth = 20
	}
	bm25Articles := bm25SearchWithIndex(allArticles, queryText, bm25Depth, si)
	bm25IDs := make([]string, len(bm25Articles))
	for i, a := range bm25Articles {
		bm25IDs[i] = a.ID
	}

	// Vector side.
	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		return nil, fmt.Errorf("load vector index: %w", err)
	}
	vecDepth := topK * 4
	if vecDepth < 20 {
		vecDepth = 20
	}
	vecHits := idx.Search(queryVec, vecDepth)
	vecIDs := make([]string, len(vecHits))
	for i, h := range vecHits {
		vecIDs[i] = h.ID
	}

	// Build an article lookup so RRF output can be materialized cheaply.
	articlesByID := make(map[string]*model.WikiArticle, len(allArticles))
	for _, a := range allArticles {
		articlesByID[a.ID] = a
	}

	fusedIDs, fusedScores, bm25RankByID, vecRankByID := rrfFuse(bm25IDs, vecIDs)

	out := make([]vectorSearchResult, 0, len(fusedIDs))
	for fusedRank, id := range fusedIDs {
		if topK > 0 && fusedRank >= topK {
			break
		}
		a, ok := articlesByID[id]
		if !ok {
			// Vector points at a deleted article. Skip without polluting output.
			continue
		}
		bm25Rank, ok1 := bm25RankByID[id]
		if !ok1 {
			bm25Rank = -1
		}
		vecRank, ok2 := vecRankByID[id]
		if !ok2 {
			vecRank = -1
		}
		out = append(out, vectorSearchResult{
			Article:   a,
			Score:     fusedScores[fusedRank],
			BM25Rank:  bm25Rank,
			VecRank:   vecRank,
			FusedRank: fusedRank,
		})
	}
	return out, nil
}
