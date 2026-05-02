// vector_cli.go — Wires vsearch.go's VectorIndex into the kb CLI surface.
// Adds three capabilities on top of the existing BM25 pipeline:
//  1. `kb ingest --vec <vec.json> --id <article_id> --scope <s>` — attach an
//     externally-computed vector to an existing article (loaded from a JSON
//     file containing either {"vector": [...]} or a bare [...] array).
//  2. `kb search --query-vec <vec.json> --scope <s> [--topk N]` — pure cosine
//     search over the per-scope vector index.
//  3. `kb search "<text>" --hybrid --query-vec <vec.json> --scope <s> [--topk N]`
//     — hybrid retrieval. BM25 with text + cosine with vector, fused via
//     reciprocal rank fusion (RRF, k=60).
//
// Per-scope vector index lives at ~/.knowledge-base/{scope}/vectors.json,
// alongside raw/ and wiki/. Lazy-loaded on first read, lazy-created on first
// --vec write. The index file is JSON written by VectorIndex.Save (see
// vsearch.go) so external tooling can inspect or hand-edit it.
//
// JSON shape contract (search results):
//   - BM25-only (existing behaviour): {id, title, summary, concepts}.
//     This file does NOT add new keys to that shape — regression-tested.
//   - Pure cosine: {id, title, summary, concepts, score, vec_rank}.
//   - Hybrid: {id, title, summary, concepts, score, bm25_rank, vec_rank, fused_rank}.
//     `score` carries the fused RRF score, ranks come from the source lists
//     (-1 means the article was not present in that list).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// rrfK is the standard reciprocal-rank-fusion constant from Cormack et al.
// 2009. Hard-coded — overriding it per-call would invite silent ranking drift
// across consumers and is not in scope.
const rrfK = 60

// vectorIndexPath returns the on-disk location for a scope's vector index.
// Mirrors the storage layout used by raw/ and wiki/ — both are subdirs under
// ~/.knowledge-base/{scope}/, the vector index is a flat sibling file.
func vectorIndexPath(scope string) string {
	return filepath.Join(scopeDir(scope), "vectors.json")
}

// loadOrCreateVectorIndex returns the on-disk index for the scope, or a fresh
// empty one if the file doesn't exist yet. Errors only on actual I/O / parse
// failures — a missing file is the expected first-write case.
func loadOrCreateVectorIndex(scope string) (*VectorIndex, error) {
	path := vectorIndexPath(scope)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return NewVectorIndex(), nil
		}
		return nil, err
	}
	return LoadVectorIndex(path)
}

// saveVectorIndex persists the vector index to ~/.knowledge-base/{scope}/vectors.json.
// Creates the parent directory if missing (matches ensureDirs idiom for raw/, wiki/).
func saveVectorIndex(scope string, idx *VectorIndex) error {
	path := vectorIndexPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return idx.Save(path)
}

// loadVectorFromFile parses a JSON file containing either:
//   - {"vector": [0.1, -0.05, ...]}  (object form)
//   - [0.1, -0.05, ...]              (bare-array form)
//
// Both encodings are accepted because callers come from heterogeneous sources
// (Python embedding scripts, hand-written test fixtures, future SDK clients).
// Returns the float32 slice or an error if the file is missing / unparseable
// / empty.
func loadVectorFromFile(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Try object form first: {"vector": [...]}
	var asObj struct {
		Vector []float32 `json:"vector"`
	}
	if err := json.Unmarshal(data, &asObj); err == nil && len(asObj.Vector) > 0 {
		return asObj.Vector, nil
	}
	// Fall back to bare-array form: [...]
	var asArr []float32
	if err := json.Unmarshal(data, &asArr); err != nil {
		return nil, fmt.Errorf("parse %s: not {\"vector\": [...]} or [...]: %w", path, err)
	}
	if len(asArr) == 0 {
		return nil, fmt.Errorf("parse %s: vector is empty", path)
	}
	return asArr, nil
}

// attachVectorToArticle is the non-fatal core of `kb ingest --vec`. Validates
// inputs, loads the vector file, upserts into the per-scope VectorIndex, and
// persists. Returns the resulting (dim, total-vectors-after) on success so
// the CLI wrapper can print a confirmation; tests call this directly to
// avoid the CLI's os.Exit-on-fatal flow.
//
// The article-existence check is intentional: if a caller mis-types the id,
// we want a hard error rather than a silent vector orphan. (Vectors keyed off
// non-existent ids would never be retrieved anyway, since search returns
// articles by id-lookup.)
func attachVectorToArticle(scope, articleID, vecPath string) (dim, total int, err error) {
	if articleID == "" {
		return 0, 0, fmt.Errorf("ingest --vec requires --id <article_id>")
	}
	if vecPath == "" {
		return 0, 0, fmt.Errorf("ingest --vec requires a vector file path")
	}
	// Confirm the article exists. Otherwise the vector would orphan and
	// hybrid search would skip it on rrfFuse's articlesByID lookup.
	if a, e := loadArticle(scope, articleID); e != nil || a == nil {
		return 0, 0, fmt.Errorf("article %q not found in scope %q (run `kb ingest` to create it first)", articleID, scope)
	}
	vec, err := loadVectorFromFile(vecPath)
	if err != nil {
		return 0, 0, fmt.Errorf("load vector: %w", err)
	}
	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		return 0, 0, fmt.Errorf("load vector index: %w", err)
	}
	idx.Add(articleID, vec)
	if err := saveVectorIndex(scope, idx); err != nil {
		return 0, 0, fmt.Errorf("save vector index: %w", err)
	}
	return len(vec), idx.Len(), nil
}

// runIngestVec is the CLI wrapper around attachVectorToArticle. Calls fatal()
// on any failure (matching the rest of cmdIngest) and emits human/JSON output
// on success.
func runIngestVec(scope, articleID, vecPath string, jsonOut bool) {
	dim, total, err := attachVectorToArticle(scope, articleID, vecPath)
	if err != nil {
		fatal("%v", err)
	}
	if jsonOut {
		printJSON(map[string]any{
			"article": articleID,
			"dim":     dim,
			"vectors": total,
		})
	} else {
		fmt.Printf("Attached %d-dim vector to %s (scope %s, %d total)\n", dim, articleID, scope, total)
	}
}

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
	Article   *WikiArticle
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
	articlesByID := make(map[string]*WikiArticle, len(allArticles))
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

// vectorIndexCount returns how many entries the per-scope vector index holds.
// 0 when the index file doesn't exist yet. Used by cmdStats to populate the
// "vectors" field. Errors are swallowed and treated as 0 — stats is best-effort.
func vectorIndexCount(scope string) int {
	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		return 0
	}
	return idx.Len()
}

// emitVectorResults renders vector / hybrid search hits to stdout. Mirrors
// the existing JSON-vs-table split in cmdSearch but with the extended row
// shape (score / bm25_rank / vec_rank / fused_rank). Only called when
// --query-vec is set, so the BM25-only output path is left intact upstream.
//
// Hybrid mode emits all four rank-related keys; pure-vec mode emits only
// `score` and `vec_rank`. We deliberately do NOT add bm25_rank=-1 to pure-vec
// rows — keeping the schema minimal makes consumers easier to write.
func emitVectorResults(results []vectorSearchResult, hybridMode, jsonOut bool) {
	if jsonOut {
		out := make([]map[string]any, 0, len(results))
		for _, r := range results {
			row := map[string]any{
				"id":       r.Article.ID,
				"title":    r.Article.Title,
				"summary":  r.Article.Summary,
				"concepts": r.Article.Concepts,
				"score":    r.Score,
			}
			if hybridMode {
				row["bm25_rank"] = r.BM25Rank
				row["vec_rank"] = r.VecRank
				row["fused_rank"] = r.FusedRank
			} else {
				row["vec_rank"] = r.VecRank
			}
			out = append(out, row)
		}
		printJSON(out)
		return
	}
	if len(results) == 0 {
		fmt.Println("No results found.")
		return
	}
	mode := "vector"
	if hybridMode {
		mode = "hybrid (BM25 + cosine, RRF k=60)"
	}
	fmt.Printf("Found %d results (%s):\n\n", len(results), mode)
	for i, r := range results {
		fmt.Printf("  %d. %s  [score=%.4f", i+1, r.Article.Title, r.Score)
		if hybridMode {
			fmt.Printf(" bm25=%d vec=%d", r.BM25Rank, r.VecRank)
		} else {
			fmt.Printf(" vec=%d", r.VecRank)
		}
		fmt.Println("]")
		if r.Article.Summary != "" {
			fmt.Printf("     %s\n", truncate(r.Article.Summary, 120))
		}
		fmt.Println()
	}
}
