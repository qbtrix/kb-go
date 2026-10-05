// Package vector is brute-force dense-vector search: cosine similarity over
// float32 slices and a flat in-memory Index with binary persistence
// (binary.go, what kb writes) and JSON persistence (legacy files), plus
// LoadFile for a query/embedding vector file ({"vector": [...]} or a bare
// array). Sufficient for <100k vectors at sub-millisecond query latency.
// Vectors are supplied externally: kb never embeds text itself. This is the
// "level 2" retrieval layer next to BM25 (level 1) and the concept graph
// (level 0). Pure leaf: stdlib only, no knowledge-base paths.
package vector

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
)

// Entry pairs a document ID with its dense vector.
type Entry struct {
	ID     string    `json:"id"`
	Vector []float32 `json:"vector"`
}

// Index is a flat in-memory vector index with brute-force cosine search.
// Not thread-safe — caller serializes access (fine for CLI usage).
type Index struct {
	Entries []Entry `json:"entries"`

	// norms[i] is sumSquares(Entries[i].Vector), loaded from a binary file;
	// nil (or stale length) means Search computes norms itself.
	norms []float64
}

// Result is a single search hit with its cosine similarity score.
type Result struct {
	ID    string  `json:"id"`
	Score float32 `json:"score"`
}

// New creates an empty vector index.
func New() *Index {
	return &Index{}
}

// Add inserts a vector with the given document ID. Overwrites if ID already exists.
func (idx *Index) Add(id string, vector []float32) {
	idx.norms = nil
	for i, e := range idx.Entries {
		if e.ID == id {
			idx.Entries[i].Vector = vector
			return
		}
	}
	idx.Entries = append(idx.Entries, Entry{ID: id, Vector: vector})
}

// Remove deletes a vector by ID. Returns true if found.
func (idx *Index) Remove(id string) bool {
	idx.norms = nil
	for i, e := range idx.Entries {
		if e.ID == id {
			idx.Entries = append(idx.Entries[:i], idx.Entries[i+1:]...)
			return true
		}
	}
	return false
}

// Search returns the top-k most similar vectors to the query, sorted by descending
// cosine similarity. Skips entries with zero-magnitude vectors.
func (idx *Index) Search(query []float32, topK int) []Result {
	if len(idx.Entries) == 0 || len(query) == 0 || topK <= 0 {
		return nil
	}

	type scored struct {
		id    string
		score float32
	}
	results := make([]scored, 0, len(idx.Entries))

	// Same float32 bits as Cosine(query, e.Vector): the query's sum of
	// squares is taken once instead of per row, and a row's comes from the
	// binary file when loaded from one.
	normQ := sumSquares(query)
	norms := idx.norms
	if len(norms) != len(idx.Entries) {
		norms = nil
	}
	for i, e := range idx.Entries {
		var normE float64
		if norms != nil {
			normE = norms[i]
		} else {
			normE = sumSquares(e.Vector)
		}
		s := cosineNormed(query, e.Vector, normQ, normE)
		if s > 0 {
			results = append(results, scored{id: e.ID, score: s})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > len(results) {
		topK = len(results)
	}

	out := make([]Result, topK)
	for i := 0; i < topK; i++ {
		out[i] = Result{ID: results[i].id, Score: results[i].score}
	}
	return out
}

// Len returns the number of entries in the index.
func (idx *Index) Len() int {
	return len(idx.Entries)
}

// Save writes the index to a JSON file.
func (idx *Index) Save(path string) error {
	data, err := json.Marshal(idx)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load reads a vector index from a JSON file.
// Returns an empty index if the file doesn't exist.
func Load(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

// Cosine computes the cosine similarity between two float32 vectors.
// Returns 0 if either vector has zero magnitude or they differ in length.
func Cosine(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		ai, bi := float64(a[i]), float64(b[i])
		dot += ai * bi
		normA += ai * ai
		normB += bi * bi
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// cosineNormed is Cosine with both sums of squares supplied.
func cosineNormed(a, b []float32, normA, normB float64) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// LoadFile parses a JSON file containing either:
//   - {"vector": [0.1, -0.05, ...]}  (object form)
//   - [0.1, -0.05, ...]              (bare-array form)
//
// Both encodings are accepted because callers come from heterogeneous sources
// (Python embedding scripts, hand-written test fixtures, future SDK clients).
// Returns the float32 slice or an error if the file is missing / unparseable
// / empty.
func LoadFile(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// os.ReadFile errors echo only the path, never file bytes — safe to keep.
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	// Try object form first: {"vector": [...]}
	var asObj struct {
		Vector []float32 `json:"vector"`
	}
	if err := json.Unmarshal(data, &asObj); err == nil && len(asObj.Vector) > 0 {
		return asObj.Vector, nil
	}
	// Fall back to bare-array form: [...]. Do NOT wrap the json error (issue
	// #23): encoding/json's messages embed the offending input bytes, which
	// would echo file contents back to an agent over the MCP surface. A generic
	// shape message is enough for a human to fix a hand-written fixture.
	var asArr []float32
	if err := json.Unmarshal(data, &asArr); err != nil {
		return nil, fmt.Errorf("parse %s: not {\"vector\": [...]} or [...]", path)
	}
	if len(asArr) == 0 {
		return nil, fmt.Errorf("parse %s: vector is empty", path)
	}
	return asArr, nil
}
