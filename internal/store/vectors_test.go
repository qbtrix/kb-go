// Tests for per-scope vector persistence: load-or-create on first use, the
// vectors.bin path surviving across invocations, a legacy vectors.json read as
// a fallback and migrated by the next write, corrupt files reported as errors
// (never as an empty index a write would then persist), AttachVector's validation
// (empty id, missing article) and happy path, and VectorCount.

package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/textutil"
	"github.com/qbtrix/kb-go/internal/vector"
)

func TestCmdIngestVec_AttachesVectorToExistingArticle(t *testing.T) {
	dir, scope := vectorTestEnv(t, "ingest")

	// Plant an article so the existence check passes.
	stubArticle(t, scope, "art-1", "First Article", "Auth flow notes.", "Body text about OAuth2.")

	vec := []float32{0.1, 0.2, 0.3, 0.4}
	vecPath := kbtest.WriteVecJSON(t, dir, "vec.json", vec, "object")

	dim, total, err := AttachVector(scope, "art-1", vecPath)
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
	idx, err := LoadVectors(scope)
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
	vecPath := kbtest.WriteVecJSON(t, dir, "vec.json", []float32{0.1, 0.2}, "array")
	if _, _, err := AttachVector(scope, "missing", vecPath); err == nil {
		t.Error("attaching to non-existent article should error")
	}
}

func TestCmdIngestVec_RejectsEmptyID(t *testing.T) {
	dir, scope := vectorTestEnv(t, "noid")
	vecPath := kbtest.WriteVecJSON(t, dir, "vec.json", []float32{0.1, 0.2}, "array")
	if _, _, err := AttachVector(scope, "", vecPath); err == nil {
		t.Error("empty id should error")
	}
}

func TestCmdStats_ReportsVectorCount(t *testing.T) {
	_, scope := vectorTestEnv(t, "stats")

	// Empty scope reports 0.
	if got := VectorCount(scope); got != 0 {
		t.Errorf("empty scope: want 0 vectors, got %d", got)
	}

	// Add three articles + vectors.
	stubArticle(t, scope, "a", "A", "summary a", "body a")
	stubArticle(t, scope, "b", "B", "summary b", "body b")
	stubArticle(t, scope, "c", "C", "summary c", "body c")

	idx, _ := LoadVectors(scope)
	idx.Add("a", []float32{1, 0})
	idx.Add("b", []float32{0, 1})
	idx.Add("c", []float32{1, 1})
	SaveVectors(scope, idx)

	if got := VectorCount(scope); got != 3 {
		t.Errorf("want 3 vectors, got %d", got)
	}
}

func TestVectorIndexPath_PersistsAcrossInvocations(t *testing.T) {
	_, scope := vectorTestEnv(t, "persist")
	stubArticle(t, scope, "doc-1", "Doc 1", "summary", "body content")

	// "First invocation": attach a vector.
	dir := t.TempDir()
	vecPath := kbtest.WriteVecJSON(t, dir, "v.json", []float32{0.1, 0.2, 0.3}, "object")
	if _, _, err := AttachVector(scope, "doc-1", vecPath); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// "Second invocation": load the index from disk fresh and confirm the
	// vector survived. We never touch the in-memory idx from the first call.
	indexPath := VectorIndexPath(scope)
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("vectors.bin should exist on disk: %v", err)
	}
	loaded, err := vector.LoadBinary(indexPath)
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

func TestLegacyVectorsJSONMigratesToBinary(t *testing.T) {
	dir, scope := vectorTestEnv(t, "migrate")
	stubArticle(t, scope, "a", "A", "s", "body")
	stubArticle(t, scope, "b", "B", "s", "body")
	legacy := vector.New()
	legacy.Add("a", []float32{0.25, -1.5, 3})
	jsonPath := filepath.Join(ScopeDir(scope), "vectors.json")
	if err := legacy.Save(jsonPath); err != nil {
		t.Fatal(err)
	}

	// Read path: the JSON file is the fallback while no .bin exists.
	idx, err := LoadVectors(scope)
	if err != nil || idx.Len() != 1 || idx.Entries[0].ID != "a" {
		t.Fatalf("legacy vectors.json not loaded: %v %+v", err, idx)
	}
	if VectorCount(scope) != 1 {
		t.Fatal("VectorCount ignored the legacy file")
	}

	// The next vector write (kb ingest --vec) migrates it.
	vecPath := kbtest.WriteVecJSON(t, dir, "vb.json", []float32{1, 2, 3}, "array")
	if _, total, err := AttachVector(scope, "b", vecPath); err != nil || total != 2 {
		t.Fatalf("attach: total=%d err=%v", total, err)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("vectors.json left behind after the migration (err=%v)", err)
	}
	got, err := vector.LoadBinary(VectorIndexPath(scope))
	if err != nil || got.Len() != 2 || got.Entries[0].ID != "a" || got.Entries[1].ID != "b" {
		t.Fatalf("migrated vectors.bin: %v %+v", err, got)
	}
	for i, x := range []float32{0.25, -1.5, 3} {
		if got.Entries[0].Vector[i] != x {
			t.Fatalf("migrated vector changed: %v", got.Entries[0].Vector)
		}
	}
}

func TestCorruptVectorFilesError(t *testing.T) {
	_, scope := vectorTestEnv(t, "corrupt")
	jsonPath := filepath.Join(ScopeDir(scope), "vectors.json")

	// Corrupt legacy JSON: an error, as it always was.
	os.WriteFile(jsonPath, []byte("{corrupt"), 0o644)
	if _, err := LoadVectors(scope); err == nil {
		t.Fatal("corrupt vectors.json loaded")
	}

	// Corrupt or truncated .bin: an error even with a valid JSON beside it
	// (the stale JSON must not resurrect), never an empty index.
	good := vector.New()
	good.Add("a", []float32{1, 2})
	good.Save(jsonPath)
	idx := vector.New()
	idx.Add("a", []float32{1, 2})
	idx.Add("b", []float32{3, 4})
	data, _ := idx.MarshalBinary()
	for name, bad := range map[string][]byte{
		"truncated": data[:len(data)-3],
		"flipped":   append(append([]byte(nil), data[:30]...), append([]byte{data[30] ^ 1}, data[31:]...)...),
		"empty":     {},
		"json":      []byte(`{"entries":[]}`),
	} {
		os.WriteFile(VectorIndexPath(scope), bad, 0o644)
		if got, err := LoadVectors(scope); err == nil {
			t.Fatalf("%s vectors.bin loaded as %d entries", name, got.Len())
		}
		if VectorCount(scope) != 0 {
			t.Fatalf("%s: VectorCount should report 0 for an unreadable index", name)
		}
	}
}

func TestLoadOrCreateVectorIndex_EmptyOnFirstCall(t *testing.T) {
	_, scope := vectorTestEnv(t, "first")
	idx, err := LoadVectors(scope)
	if err != nil {
		t.Fatalf("loadOrCreate: %v", err)
	}
	if idx.Len() != 0 {
		t.Errorf("first call should return empty index, got len=%d", idx.Len())
	}
}

// vectorTestEnv sets HOME to a fresh temp dir so BaseDir() routes into it.
// Returns the temp dir and a fresh scope name. The scope name varies per test
// so accidental cross-test pollution surfaces immediately.
func vectorTestEnv(t *testing.T, name string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "vec-" + name + "-" + filepath.Base(dir)
	EnsureDirs(scope)
	return dir, scope
}

// stubArticle plants a minimal WikiArticle with the given id so that
// AttachVector's existence check passes and search can resolve hits.
func stubArticle(t *testing.T, scope, id, title, summary string, content string) {
	t.Helper()
	a := &model.WikiArticle{
		ID:           id,
		Title:        title,
		Summary:      summary,
		Content:      content,
		Concepts:     []string{},
		Categories:   []string{},
		SourceDocs:   []string{},
		Backlinks:    []string{},
		WordCount:    textutil.WordCount(content),
		CompiledAt:   "2026-04-30T00:00:00Z",
		CompiledWith: "test",
		Version:      1,
	}
	if err := SaveArticle(scope, a); err != nil {
		t.Fatalf("saveArticle %s: %v", id, err)
	}
}
