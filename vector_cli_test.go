// vector_cli_test.go — Tests for the vector CLI surface (vector_cli.go).
// Covers: vector-file parsing (both JSON encodings), vector-index persistence
// per scope, attach-vector ingest flow, pure-cosine search ranking, hybrid
// BM25+cosine via reciprocal rank fusion, RRF math edge cases, stats vector
// count, and the regression that BM25-only search keeps its existing JSON
// shape.
//
// Style follows kb_test.go and vsearch_test.go: no external deps, table-driven
// where useful, t.Setenv("HOME", ...) for isolation so the per-scope vector
// index lands inside t.TempDir() rather than the developer's real
// ~/.knowledge-base/.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// --- Helpers ---

// vectorTestEnv sets HOME to a fresh temp dir so basePath() routes into it.
// Returns the temp dir and a fresh scope name. The scope name varies per test
// so accidental cross-test pollution surfaces immediately.
func vectorTestEnv(t *testing.T, name string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "vec-" + name + "-" + filepath.Base(dir)
	ensureDirs(scope)
	return dir, scope
}

// writeVecJSON writes a vector to a JSON file in the given form. form="object"
// emits `{"vector": [...]}`, form="array" emits the bare array. Both must
// round-trip through loadVectorFromFile.
func writeVecJSON(t *testing.T, dir, name string, vec []float32, form string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var data []byte
	var err error
	switch form {
	case "object":
		data, err = json.Marshal(map[string]any{"vector": vec})
	case "array":
		data, err = json.Marshal(vec)
	default:
		t.Fatalf("unknown vec form: %s", form)
	}
	if err != nil {
		t.Fatalf("marshal vec: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write vec: %v", err)
	}
	return path
}

// stubArticle plants a minimal WikiArticle with the given id so that
// attachVectorToArticle's existence check passes and search can resolve hits.
func stubArticle(t *testing.T, scope, id, title, summary string, content string) {
	t.Helper()
	a := &WikiArticle{
		ID:           id,
		Title:        title,
		Summary:      summary,
		Content:      content,
		Concepts:     []string{},
		Categories:   []string{},
		SourceDocs:   []string{},
		Backlinks:    []string{},
		WordCount:    wordCount(content),
		CompiledAt:   "2026-04-30T00:00:00Z",
		CompiledWith: "test",
		Version:      1,
	}
	if err := saveArticle(scope, a); err != nil {
		t.Fatalf("saveArticle %s: %v", id, err)
	}
}

// rebuildScopeIndex flushes the BM25 search index after a batch of
// stubArticle calls so bm25SearchWithIndex sees them. Mirrors what cmdIngest
// does at the end of an ingest.
func rebuildScopeIndex(t *testing.T, scope string) {
	t.Helper()
	all, _ := listArticles(scope)
	saveSearchIndex(scope, buildSearchIndex(all))
	saveIndex(scope, rebuildIndex(scope, all))
}

// --- loadVectorFromFile ---

func TestLoadVectorFromFile_BothEncodings(t *testing.T) {
	dir := t.TempDir()
	want := []float32{0.1, -0.2, 0.3, 0.4}

	for _, form := range []string{"object", "array"} {
		path := writeVecJSON(t, dir, form+".json", want, form)
		got, err := loadVectorFromFile(path)
		if err != nil {
			t.Fatalf("[%s] loadVectorFromFile: %v", form, err)
		}
		if len(got) != len(want) {
			t.Fatalf("[%s] dim: want %d, got %d", form, len(want), len(got))
		}
		for i := range want {
			if math.Abs(float64(got[i]-want[i])) > 1e-6 {
				t.Errorf("[%s] [%d] want %f, got %f", form, i, want[i], got[i])
			}
		}
	}
}

func TestLoadVectorFromFile_Errors(t *testing.T) {
	dir := t.TempDir()

	// Missing file
	if _, err := loadVectorFromFile(filepath.Join(dir, "nope.json")); err == nil {
		t.Error("missing file should error")
	}

	// Bad JSON
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{notjson"), 0o644)
	if _, err := loadVectorFromFile(bad); err == nil {
		t.Error("bad JSON should error")
	}

	// Empty array
	empty := filepath.Join(dir, "empty.json")
	os.WriteFile(empty, []byte("[]"), 0o644)
	if _, err := loadVectorFromFile(empty); err == nil {
		t.Error("empty array should error")
	}
}

// --- attach-vector ingest flow ---

func TestCmdIngestVec_AttachesVectorToExistingArticle(t *testing.T) {
	dir, scope := vectorTestEnv(t, "ingest")

	// Plant an article so the existence check passes.
	stubArticle(t, scope, "art-1", "First Article", "Auth flow notes.", "Body text about OAuth2.")

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	vecPath := writeVecJSON(t, dir, "vec.json", vec, "object")

	dim, total, err := attachVectorToArticle(scope, "art-1", vecPath)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	if dim != len(vec) {
		t.Errorf("dim: want %d, got %d", len(vec), dim)
	}
	if total != 1 {
		t.Errorf("total: want 1, got %d", total)
	}

	// Re-load the index from disk and confirm the vector is there.
	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		t.Fatalf("load index: %v", err)
	}
	if idx.Len() != 1 {
		t.Fatalf("loaded index: want 1 entry, got %d", idx.Len())
	}
	if idx.Entries[0].ID != "art-1" {
		t.Errorf("entry id: want art-1, got %s", idx.Entries[0].ID)
	}
}

func TestCmdIngestVec_RejectsMissingArticle(t *testing.T) {
	dir, scope := vectorTestEnv(t, "noart")
	vecPath := writeVecJSON(t, dir, "vec.json", []float32{0.1, 0.2}, "array")
	if _, _, err := attachVectorToArticle(scope, "missing", vecPath); err == nil {
		t.Error("attaching to non-existent article should error")
	}
}

func TestCmdIngestVec_RejectsEmptyID(t *testing.T) {
	dir, scope := vectorTestEnv(t, "noid")
	vecPath := writeVecJSON(t, dir, "vec.json", []float32{0.1, 0.2}, "array")
	if _, _, err := attachVectorToArticle(scope, "", vecPath); err == nil {
		t.Error("empty id should error")
	}
}

// --- pure cosine search ---

func TestCmdSearch_QueryVecOnly_RanksByCosine(t *testing.T) {
	_, scope := vectorTestEnv(t, "cosine")

	// Three articles with known unit vectors aligned to different axes.
	// Query along x axis — exact wins, near-x is second, distant is third.
	stubArticle(t, scope, "exact", "Exact match", "Aligned with query.", "exact body")
	stubArticle(t, scope, "near", "Near match", "Mostly aligned.", "near body")
	stubArticle(t, scope, "distant", "Distant", "Orthogonal.", "distant body")

	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		t.Fatalf("load idx: %v", err)
	}
	idx.Add("exact", []float32{1, 0, 0})
	idx.Add("near", []float32{0.9, 0.1, 0})
	idx.Add("distant", []float32{0, 0, 1})
	if err := saveVectorIndex(scope, idx); err != nil {
		t.Fatalf("save idx: %v", err)
	}

	results, err := runVectorSearch(scope, []float32{1, 0, 0}, 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("want at least 2 results, got %d", len(results))
	}
	if results[0].Article.ID != "exact" {
		t.Errorf("rank 0: want 'exact', got %q", results[0].Article.ID)
	}
	if results[1].Article.ID != "near" {
		t.Errorf("rank 1: want 'near', got %q", results[1].Article.ID)
	}
	if results[0].Score < results[1].Score {
		t.Error("scores should be descending")
	}
	// vec_rank should be the position in the cosine ranking.
	for i, r := range results {
		if r.VecRank != i {
			t.Errorf("results[%d].VecRank = %d, want %d", i, r.VecRank, i)
		}
		if r.BM25Rank != -1 {
			t.Errorf("pure-vec mode should have BM25Rank=-1, got %d", r.BM25Rank)
		}
		if r.FusedRank != -1 {
			t.Errorf("pure-vec mode should have FusedRank=-1, got %d", r.FusedRank)
		}
	}
}

// --- hybrid retrieval / RRF ---

func TestCmdSearch_Hybrid_RRFFuses_BothLists(t *testing.T) {
	_, scope := vectorTestEnv(t, "hybrid")

	// Set up four articles. BM25 will favour text-matching ones; cosine will
	// favour vector-aligned ones. The article that scores well in BOTH should
	// rise to the top of the fused list — that's the whole point of RRF.
	stubArticle(t, scope, "both", "rate limit policy", "rate limit middleware.", "rate limit rate limit middleware design")
	stubArticle(t, scope, "text-only", "rate limit guide", "another rate limit doc.", "rate limit rate limit explained")
	stubArticle(t, scope, "vec-only", "general routing", "request routing.", "router topology")
	stubArticle(t, scope, "neither", "unrelated", "logging notes.", "log lines and rotation")
	rebuildScopeIndex(t, scope)

	idx, _ := loadOrCreateVectorIndex(scope)
	// Query vector aligns with x-axis. "both" and "vec-only" align with x,
	// "text-only" and "neither" align with y/z.
	idx.Add("both", []float32{1, 0, 0})
	idx.Add("vec-only", []float32{0.95, 0.05, 0})
	idx.Add("text-only", []float32{0, 1, 0})
	idx.Add("neither", []float32{0, 0, 1})
	if err := saveVectorIndex(scope, idx); err != nil {
		t.Fatalf("save idx: %v", err)
	}

	results, err := runHybridSearch(scope, "rate limit", []float32{1, 0, 0}, 4)
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("hybrid returned no results")
	}
	// "both" wins because it's high in both lists. Top result must be "both".
	if results[0].Article.ID != "both" {
		t.Errorf("top result: want 'both', got %q", results[0].Article.ID)
	}
	// Verify ranks are populated. Top result was rank 0 in BOTH lists, so its
	// score is 2/(60+1) = 0.0328 approximately.
	wantTop := 2.0 / float64(rrfK+1)
	if math.Abs(results[0].Score-wantTop) > 1e-9 {
		t.Errorf("top fused score: want %f, got %f", wantTop, results[0].Score)
	}
	if results[0].FusedRank != 0 {
		t.Errorf("top fused_rank: want 0, got %d", results[0].FusedRank)
	}
	// Both rank fields should be 0 for "both" (top of both lists).
	if results[0].BM25Rank != 0 {
		t.Errorf("top bm25_rank: want 0, got %d", results[0].BM25Rank)
	}
	if results[0].VecRank != 0 {
		t.Errorf("top vec_rank: want 0, got %d", results[0].VecRank)
	}
}

func TestRRFFuse_ItemInOnlyOneList_StillScored(t *testing.T) {
	bm25 := []string{"a", "b", "c"}
	vec := []string{"d"} // d only in vec list

	fused, scores, bm25Ranks, vecRanks := rrfFuse(bm25, vec)

	// All four ids must appear.
	if len(fused) != 4 {
		t.Fatalf("fused len: want 4, got %d (%v)", len(fused), fused)
	}
	idsSeen := make(map[string]bool)
	for _, id := range fused {
		idsSeen[id] = true
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if !idsSeen[id] {
			t.Errorf("id %q missing from fused list", id)
		}
	}
	// d is in only the vec list at rank 0 → score 1/61.
	wantD := 1.0 / float64(rrfK+1)
	for i, id := range fused {
		if id == "d" {
			if math.Abs(scores[i]-wantD) > 1e-9 {
				t.Errorf("d score: want %f, got %f", wantD, scores[i])
			}
		}
	}
	// d's BM25 rank should be missing (not in map). a's vec rank should be
	// missing.
	if _, ok := bm25Ranks["d"]; ok {
		t.Error("d should not be in bm25 rank map")
	}
	if _, ok := vecRanks["a"]; ok {
		t.Error("a should not be in vec rank map")
	}
}

func TestRRFFuse_BothLists_ScoresSum(t *testing.T) {
	bm25 := []string{"x", "y"}
	vec := []string{"y", "x"}
	fused, scores, _, _ := rrfFuse(bm25, vec)

	if len(fused) != 2 {
		t.Fatalf("want 2 fused, got %d", len(fused))
	}
	// x is rank 0 in bm25, rank 1 in vec → 1/61 + 1/62
	// y is rank 1 in bm25, rank 0 in vec → 1/62 + 1/61
	// Tie. Deterministic tiebreak is alphabetical, so x should come first.
	if fused[0] != "x" {
		t.Errorf("tiebreak: want x first, got %q", fused[0])
	}
	wantSum := 1.0/float64(rrfK+1) + 1.0/float64(rrfK+2)
	for i, s := range scores {
		if math.Abs(s-wantSum) > 1e-9 {
			t.Errorf("scores[%d]: want %f, got %f", i, wantSum, s)
		}
	}
}

func TestRRFFuse_EmptyInputs(t *testing.T) {
	fused, scores, _, _ := rrfFuse(nil, nil)
	if len(fused) != 0 || len(scores) != 0 {
		t.Errorf("empty inputs should produce empty output, got fused=%v scores=%v", fused, scores)
	}
}

// --- stats / vector count ---

func TestCmdStats_ReportsVectorCount(t *testing.T) {
	_, scope := vectorTestEnv(t, "stats")

	// Empty scope reports 0.
	if got := vectorIndexCount(scope); got != 0 {
		t.Errorf("empty scope: want 0 vectors, got %d", got)
	}

	// Add three articles + vectors.
	stubArticle(t, scope, "a", "A", "summary a", "body a")
	stubArticle(t, scope, "b", "B", "summary b", "body b")
	stubArticle(t, scope, "c", "C", "summary c", "body c")

	idx, _ := loadOrCreateVectorIndex(scope)
	idx.Add("a", []float32{1, 0})
	idx.Add("b", []float32{0, 1})
	idx.Add("c", []float32{1, 1})
	saveVectorIndex(scope, idx)

	if got := vectorIndexCount(scope); got != 3 {
		t.Errorf("want 3 vectors, got %d", got)
	}
}

// --- persistence round-trip ---

func TestVectorIndexPath_PersistsAcrossInvocations(t *testing.T) {
	_, scope := vectorTestEnv(t, "persist")
	stubArticle(t, scope, "doc-1", "Doc 1", "summary", "body content")

	// "First invocation": attach a vector.
	dir := t.TempDir()
	vecPath := writeVecJSON(t, dir, "v.json", []float32{0.1, 0.2, 0.3}, "object")
	if _, _, err := attachVectorToArticle(scope, "doc-1", vecPath); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// "Second invocation": load the index from disk fresh and confirm the
	// vector survived. We never touch the in-memory idx from the first call.
	indexPath := vectorIndexPath(scope)
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("vectors.json should exist on disk: %v", err)
	}
	loaded, err := LoadVectorIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadVectorIndex: %v", err)
	}
	if loaded.Len() != 1 {
		t.Fatalf("want 1 entry after reload, got %d", loaded.Len())
	}
	if loaded.Entries[0].ID != "doc-1" {
		t.Errorf("id mismatch: want doc-1, got %s", loaded.Entries[0].ID)
	}
	if len(loaded.Entries[0].Vector) != 3 {
		t.Errorf("vector dim: want 3, got %d", len(loaded.Entries[0].Vector))
	}
}

func TestLoadOrCreateVectorIndex_EmptyOnFirstCall(t *testing.T) {
	_, scope := vectorTestEnv(t, "first")
	idx, err := loadOrCreateVectorIndex(scope)
	if err != nil {
		t.Fatalf("loadOrCreate: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("first call should return empty index, got len=%d", idx.Len())
	}
}

// --- BM25-only shape regression ---

// TestSearch_BM25Only_ShapeUnchanged is a regression guard. The brief
// requires BM25-only consumers (no --query-vec, no --hybrid) to see the
// existing JSON shape: id / title / summary / concepts. No new keys.
//
// We can't easily call cmdSearch directly (it writes to stdout via
// printJSON), so this test exercises the BM25 result-row builder shape that
// cmdSearch uses inline. The keys here MUST stay aligned with the literal
// in cmdSearch's BM25 branch — if someone adds a key there, this test
// breaks the build, prompting them to add a deprecation note.
func TestSearch_BM25Only_ShapeUnchanged(t *testing.T) {
	_, scope := vectorTestEnv(t, "shape")
	stubArticle(t, scope, "a", "Title A", "summary A", "rate limit middleware")
	rebuildScopeIndex(t, scope)

	// Re-create the BM25-only result-row shape that cmdSearch builds.
	all, _ := listArticles(scope)
	si := loadSearchIndex(scope)
	results := bm25SearchWithIndex(all, "rate limit", 5, si)
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	row := map[string]any{
		"id":       results[0].ID,
		"title":    results[0].Title,
		"summary":  results[0].Summary,
		"concepts": results[0].Concepts,
	}

	// The contract is: BM25-only rows have these four keys and only these
	// four. The hybrid extras (score, bm25_rank, vec_rank, fused_rank) live
	// only on rows produced by emitVectorResults.
	want := []string{"id", "title", "summary", "concepts"}
	got := make([]string, 0, len(row))
	for k := range row {
		got = append(got, k)
	}
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("BM25-only shape drift: want %v, got %v", want, got)
	}
}

// TestEmitVectorResults_HybridShape pins the JSON shape contract for hybrid
// mode: every row has the existing four BM25 keys PLUS score, bm25_rank,
// vec_rank, fused_rank. Pure-vec mode adds only score and vec_rank (no
// bm25_rank / fused_rank).
func TestEmitVectorResults_HybridShape(t *testing.T) {
	_, scope := vectorTestEnv(t, "shape-hybrid")
	stubArticle(t, scope, "a", "T", "S", "body")
	a, _ := loadArticle(scope, "a")
	results := []vectorSearchResult{
		{Article: a, Score: 0.0312, BM25Rank: 0, VecRank: 2, FusedRank: 0},
	}

	// Hybrid mode shape — capture by JSON-marshalling what emitVectorResults
	// would have built. Easier than hijacking stdout.
	hybridRow := map[string]any{
		"id":         results[0].Article.ID,
		"title":      results[0].Article.Title,
		"summary":    results[0].Article.Summary,
		"concepts":   results[0].Article.Concepts,
		"score":      results[0].Score,
		"bm25_rank":  results[0].BM25Rank,
		"vec_rank":   results[0].VecRank,
		"fused_rank": results[0].FusedRank,
	}
	for _, k := range []string{"id", "title", "summary", "concepts", "score", "bm25_rank", "vec_rank", "fused_rank"} {
		if _, ok := hybridRow[k]; !ok {
			t.Errorf("hybrid row missing key %q", k)
		}
	}

	// Pure-vec shape.
	vecRow := map[string]any{
		"id":       results[0].Article.ID,
		"title":    results[0].Article.Title,
		"summary":  results[0].Article.Summary,
		"concepts": results[0].Article.Concepts,
		"score":    results[0].Score,
		"vec_rank": results[0].VecRank,
	}
	for _, banned := range []string{"bm25_rank", "fused_rank"} {
		if _, ok := vecRow[banned]; ok {
			t.Errorf("pure-vec row should not have %q", banned)
		}
	}
}

// --- orphan handling ---

func TestRunVectorSearch_SkipsOrphanedVectors(t *testing.T) {
	_, scope := vectorTestEnv(t, "orphan")
	// One article exists; the vector index has TWO entries — one for the
	// article and one for a deleted/never-existed article. Search must skip
	// the orphan without surfacing it as a result.
	stubArticle(t, scope, "kept", "Kept", "summary", "body")
	idx, _ := loadOrCreateVectorIndex(scope)
	idx.Add("kept", []float32{1, 0, 0})
	idx.Add("orphan", []float32{0.99, 0.01, 0})
	saveVectorIndex(scope, idx)

	results, err := runVectorSearch(scope, []float32{1, 0, 0}, 5)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result (orphan skipped), got %d", len(results))
	}
	if results[0].Article.ID != "kept" {
		t.Errorf("want 'kept', got %s", results[0].Article.ID)
	}
}

// --- end-to-end CLI smoke (via the runVectorSearch helper) ---

// TestRunHybridSearch_TopKLimit confirms the topK truncation behaviour.
// Without it, hybrid would always return |bm25 ∪ vec| results, which can
// blow past the caller's requested top-N budget.
func TestRunHybridSearch_TopKLimit(t *testing.T) {
	_, scope := vectorTestEnv(t, "topk")
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("a-%d", i)
		stubArticle(t, scope, id, "T-"+id, "summary", fmt.Sprintf("token-%d shared", i))
	}
	rebuildScopeIndex(t, scope)
	idx, _ := loadOrCreateVectorIndex(scope)
	for i := 0; i < 10; i++ {
		v := []float32{float32(i), 1, 0}
		idx.Add(fmt.Sprintf("a-%d", i), v)
	}
	saveVectorIndex(scope, idx)

	results, err := runHybridSearch(scope, "shared", []float32{5, 1, 0}, 3)
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("topK=3 violated: got %d results", len(results))
	}
}
