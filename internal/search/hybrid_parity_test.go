// Hybrid search parity: HybridSearch (healing index, vectors.bin, only the
// fused top-k read from disk) returns exactly the hits of the pre-binary
// pipeline (full listing, v3 BM25, per-row Cosine over the JSON index, the
// same RRF): same ids, fused scores, bm25/vec/fused ranks and articles, across
// many queries, top-k edges, RRF ties, orphan vectors, rows of another dim and
// a query whose dim matches no row.

package search

import (
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/vector"
)

// legacyVecSearch is vector.Index.Search as it was: Cosine per row.
func legacyVecSearch(entries []vector.Entry, query []float32, topK int) []string {
	if len(entries) == 0 || len(query) == 0 || topK <= 0 {
		return nil
	}
	type scored struct {
		id    string
		score float32
	}
	var results []scored
	for _, e := range entries {
		if s := vector.Cosine(query, e.Vector); s > 0 {
			results = append(results, scored{e.ID, s})
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
	var ids []string
	for i := 0; i < min(topK, len(results)); i++ {
		ids = append(ids, results[i].id)
	}
	return ids
}

// legacyHybrid is the pre-binary HybridSearch over a fresh index.
func legacyHybrid(ls *legacyScope, entries []vector.Entry, query string, qvec []float32, topK int) []Hit {
	depth := max(topK*4, 20)
	bm25IDs, _ := ls.search(query, depth, "")
	vecIDs := legacyVecSearch(entries, qvec, depth)
	byID := map[string]*model.WikiArticle{}
	for _, a := range ls.all {
		byID[a.ID] = a
	}
	fused, scores, bRank, vRank := rrfFuse(bm25IDs, vecIDs)
	var out []Hit
	for fr, id := range fused {
		if topK > 0 && fr >= topK {
			break
		}
		a, ok := byID[id]
		if !ok {
			continue
		}
		br, ok := bRank[id]
		if !ok {
			br = -1
		}
		vr, ok := vRank[id]
		if !ok {
			vr = -1
		}
		out = append(out, Hit{Article: a, Score: scores[fr], BM25Rank: br, VecRank: vr, FusedRank: fr})
	}
	return out
}

func TestHybridSearchParityWithV3Pipeline(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "hybrid-parity"
	all := seedScope(t, scope, parityCorpus(80, 11))
	r := rand.New(rand.NewSource(12))
	const dim = 48
	idx := vector.New()
	for i, a := range all {
		if i%5 == 4 {
			continue // some articles have no vector
		}
		v := make([]float32, dim)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		idx.Add(a.ID, v)
	}
	idx.Add("zzz-orphan", make([]float32, dim)) // no such article; zero row
	orphan := make([]float32, dim)
	orphan[0] = 1
	idx.Add("aaa-orphan", orphan)
	idx.Add(all[0].ID+"-short", []float32{1, 2, 3}) // another dim
	if err := store.SaveVectors(scope, idx); err != nil {
		t.Fatal(err)
	}
	backdate(t, scope, time.Hour)
	ls := newLegacyScope(all)

	ties, compared := 0, 0
	for qi, q := range parityQueries(12) {
		qvec := make([]float32, dim)
		for j := range qvec {
			qvec[j] = float32(r.NormFloat64())
		}
		if qi%9 == 0 {
			qvec = qvec[:dim-1] // matches no row: vector side empty, as before
		}
		if qi%11 == 0 {
			qvec[0] += 50 // the orphan unit row ranks first on the vector side
		}
		for _, topK := range []int{10, 3, 1, 0} {
			want := legacyHybrid(ls, idx.Entries, q, qvec, topK)
			got, err := HybridSearch(scope, q, qvec, topK)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("q=%q topK=%d: %d hits, want %d", q, topK, len(got), len(want))
			}
			for i := range want {
				g, w := got[i], want[i]
				if g.Article.ID != w.Article.ID || g.Article.Content != w.Article.Content ||
					math.Float64bits(g.Score) != math.Float64bits(w.Score) ||
					g.BM25Rank != w.BM25Rank || g.VecRank != w.VecRank || g.FusedRank != w.FusedRank {
					t.Fatalf("q=%q topK=%d rank %d: got %s %v b%d v%d f%d, want %s %v b%d v%d f%d", q, topK, i,
						g.Article.ID, g.Score, g.BM25Rank, g.VecRank, g.FusedRank,
						w.Article.ID, w.Score, w.BM25Rank, w.VecRank, w.FusedRank)
				}
				if i > 0 && w.Score == want[i-1].Score {
					ties++
				}
				compared++
			}
		}
	}
	if ties == 0 || compared < 500 {
		t.Fatalf("fixture too weak: %d ties over %d compared hits", ties, compared)
	}
	t.Logf("compared %d hybrid hits, %d RRF ties", compared, ties)
}

// A scope with vectors but no wiki/ yields no hits (every fused id is
// unknown), as before; a corrupt vectors.bin is an error, not an empty list.
func TestHybridSearchEdgeCases(t *testing.T) {
	kbtest.SetHome(t, t.TempDir())
	scope := "hybrid-edge"
	idx := vector.New()
	idx.Add("a", []float32{1, 0})
	if err := store.SaveVectors(scope, idx); err != nil {
		t.Fatal(err)
	}
	if hits, err := HybridSearch(scope, "a", []float32{1, 0}, 5); err != nil || len(hits) != 0 {
		t.Fatalf("no wiki: hits=%v err=%v", hits, err)
	}
	seedScope(t, scope, []*model.WikiArticle{{ID: "a", Title: "Alpha"}})
	store.SaveVectors(scope, idx)
	hits, err := HybridSearch(scope, "alpha", []float32{1, 0}, 5)
	if err != nil || len(hits) != 1 || hits[0].BM25Rank != 0 || hits[0].VecRank != 0 {
		t.Fatalf("hits=%+v err=%v", hits, err)
	}
	writeCorrupt := func() {
		data, _ := idx.MarshalBinary()
		data[len(data)-1] ^= 0xff
		if err := writeFileAtomic(store.VectorIndexPath(scope), data); err != nil {
			t.Fatal(err)
		}
	}
	writeCorrupt()
	if _, err := HybridSearch(scope, "alpha", []float32{1, 0}, 5); err == nil {
		t.Fatal("corrupt vectors.bin did not error")
	}
	if _, err := VectorSearch(scope, []float32{1, 0}, 5); err == nil {
		t.Fatal("corrupt vectors.bin did not error in pure vector search")
	}
}
