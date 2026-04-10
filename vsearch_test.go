// vsearch_test.go — Tests for brute-force vector search.
// Covers cosine similarity, index CRUD, search ranking, persistence, and edge cases.
package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// --- CosineSimilarity ---

func TestCosineSimilarity_Identical(t *testing.T) {
	a := []float32{1, 2, 3}
	got := CosineSimilarity(a, a)
	if math.Abs(float64(got)-1.0) > 1e-6 {
		t.Errorf("identical vectors: want 1.0, got %f", got)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	a := []float32{1, 0}
	b := []float32{0, 1}
	got := CosineSimilarity(a, b)
	if math.Abs(float64(got)) > 1e-6 {
		t.Errorf("orthogonal vectors: want 0.0, got %f", got)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	a := []float32{1, 2, 3}
	b := []float32{-1, -2, -3}
	got := CosineSimilarity(a, b)
	if math.Abs(float64(got)+1.0) > 1e-6 {
		t.Errorf("opposite vectors: want -1.0, got %f", got)
	}
}

func TestCosineSimilarity_DifferentLengths(t *testing.T) {
	a := []float32{1, 2}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("mismatched dimensions: want 0.0, got %f", got)
	}
}

func TestCosineSimilarity_Empty(t *testing.T) {
	got := CosineSimilarity(nil, nil)
	if got != 0 {
		t.Errorf("nil vectors: want 0.0, got %f", got)
	}
}

func TestCosineSimilarity_ZeroMagnitude(t *testing.T) {
	a := []float32{0, 0, 0}
	b := []float32{1, 2, 3}
	got := CosineSimilarity(a, b)
	if got != 0 {
		t.Errorf("zero-magnitude vector: want 0.0, got %f", got)
	}
}

// --- VectorIndex ---

func TestVectorIndex_AddAndLen(t *testing.T) {
	idx := NewVectorIndex()
	if idx.Len() != 0 {
		t.Fatal("new index should be empty")
	}
	idx.Add("a", []float32{1, 0, 0})
	idx.Add("b", []float32{0, 1, 0})
	if idx.Len() != 2 {
		t.Errorf("want 2 entries, got %d", idx.Len())
	}
}

func TestVectorIndex_AddOverwrite(t *testing.T) {
	idx := NewVectorIndex()
	idx.Add("a", []float32{1, 0, 0})
	idx.Add("a", []float32{0, 1, 0}) // overwrite
	if idx.Len() != 1 {
		t.Errorf("overwrite should keep 1 entry, got %d", idx.Len())
	}
	if idx.Entries[0].Vector[1] != 1 {
		t.Error("overwrite did not update vector")
	}
}

func TestVectorIndex_Remove(t *testing.T) {
	idx := NewVectorIndex()
	idx.Add("a", []float32{1, 0})
	idx.Add("b", []float32{0, 1})
	if !idx.Remove("a") {
		t.Error("remove existing should return true")
	}
	if idx.Len() != 1 {
		t.Errorf("after remove: want 1 entry, got %d", idx.Len())
	}
	if idx.Remove("nonexistent") {
		t.Error("remove missing should return false")
	}
}

func TestVectorIndex_Search_RankedOrder(t *testing.T) {
	idx := NewVectorIndex()
	idx.Add("exact", []float32{1, 0, 0})
	idx.Add("similar", []float32{0.9, 0.1, 0})
	idx.Add("distant", []float32{0, 0, 1})

	query := []float32{1, 0, 0}
	results := idx.Search(query, 3)

	if len(results) < 2 {
		t.Fatalf("want at least 2 results, got %d", len(results))
	}
	if results[0].ID != "exact" {
		t.Errorf("top result should be 'exact', got '%s'", results[0].ID)
	}
	if results[1].ID != "similar" {
		t.Errorf("second result should be 'similar', got '%s'", results[1].ID)
	}
	if results[0].Score < results[1].Score {
		t.Error("results should be sorted by descending score")
	}
}

func TestVectorIndex_Search_TopK(t *testing.T) {
	idx := NewVectorIndex()
	for i := 0; i < 100; i++ {
		v := make([]float32, 4)
		v[i%4] = 1
		idx.Add(fmt.Sprintf("doc%d", i), v)
	}
	if idx.Len() != 100 {
		t.Fatalf("want 100 entries, got %d", idx.Len())
	}

	results := idx.Search([]float32{1, 0, 0, 0}, 5)
	if len(results) > 5 {
		t.Errorf("topK=5 but got %d results", len(results))
	}
}

func TestVectorIndex_Search_Empty(t *testing.T) {
	idx := NewVectorIndex()
	results := idx.Search([]float32{1, 0}, 10)
	if results != nil {
		t.Error("search on empty index should return nil")
	}
}

func TestVectorIndex_Search_EmptyQuery(t *testing.T) {
	idx := NewVectorIndex()
	idx.Add("a", []float32{1, 0})
	results := idx.Search(nil, 10)
	if results != nil {
		t.Error("search with nil query should return nil")
	}
}

// --- Persistence ---

func TestVectorIndex_SaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vectors.json")

	idx := NewVectorIndex()
	idx.Add("doc1", []float32{0.1, 0.2, 0.3})
	idx.Add("doc2", []float32{0.4, 0.5, 0.6})
	if err := idx.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadVectorIndex(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Len() != 2 {
		t.Errorf("loaded index should have 2 entries, got %d", loaded.Len())
	}

	// Verify round-trip fidelity
	results := loaded.Search([]float32{0.1, 0.2, 0.3}, 1)
	if len(results) == 0 || results[0].ID != "doc1" {
		t.Error("loaded index should find doc1 as top match")
	}
}

func TestLoadVectorIndex_MissingFile(t *testing.T) {
	idx, err := LoadVectorIndex("/nonexistent/path.json")
	if err != nil {
		t.Fatalf("missing file should return empty index, got error: %v", err)
	}
	if idx.Len() != 0 {
		t.Error("missing file should return empty index")
	}
}

func TestLoadVectorIndex_BadJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("{corrupt"), 0644)

	_, err := LoadVectorIndex(path)
	if err == nil {
		t.Error("bad JSON should return error")
	}
}

// --- Benchmarks ---

func BenchmarkCosineSimilarity_128d(b *testing.B) {
	a := make([]float32, 128)
	q := make([]float32, 128)
	for i := range a {
		a[i] = float32(i) * 0.01
		q[i] = float32(128-i) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		CosineSimilarity(a, q)
	}
}

func BenchmarkVectorSearch_1k_128d(b *testing.B) {
	idx := NewVectorIndex()
	for i := 0; i < 1000; i++ {
		v := make([]float32, 128)
		for j := range v {
			v[j] = float32((i*7 + j*13) % 100) * 0.01
		}
		idx.Add(string(rune(i)), v)
	}
	query := make([]float32, 128)
	for j := range query {
		query[j] = float32(j) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(query, 10)
	}
}

func BenchmarkVectorSearch_10k_384d(b *testing.B) {
	idx := NewVectorIndex()
	for i := 0; i < 10000; i++ {
		v := make([]float32, 384)
		for j := range v {
			v[j] = float32((i*7 + j*13) % 100) * 0.01
		}
		idx.Add(string(rune(i)), v)
	}
	query := make([]float32, 384)
	for j := range query {
		query[j] = float32(j) * 0.01
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idx.Search(query, 10)
	}
}
