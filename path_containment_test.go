// path_containment_test.go — Traversal containment for the MCP read path
// (issue #23): kb_search's agent-controlled query_vec_path must be refused
// outside the knowledge base and accepted inside it. LoadArticle's id guard
// and vector.LoadFile's no-leak parse errors are tested in internal/store and
// internal/vector.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
	"github.com/qbtrix/kb-go/internal/vector"
)

// --- loadVectorFromFile / MCP query_vec_path containment ---

func TestMCPSearch_QueryVecPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "vec-contain-" + filepath.Base(dir)
	store.EnsureDirs(scope)

	// Plant a readable JSON file OUTSIDE the kb base so a traversal path could
	// load it. Its contents are a valid vector so a successful read would parse
	// and the only thing stopping it is containment.
	outside := filepath.Join(dir, "outside-vector.json")
	if err := os.WriteFile(outside, []byte(`{"vector":[0.1,0.2],"marker":"LEAKED-CONTENTS"}`), 0o644); err != nil {
		t.Fatalf("plant outside vec: %v", err)
	}

	args := map[string]any{
		"scope":          scope,
		"query_vec_path": outside,
	}
	_, err := mcpSearch(newArticleCache(), args, scope)
	if err == nil {
		t.Fatalf("mcpSearch accepted an out-of-base query_vec_path; traversal not contained")
	}
	if strings.Contains(err.Error(), "LEAKED-CONTENTS") {
		t.Fatalf("mcpSearch leaked file contents in error: %v", err)
	}
}

func TestMCPSearch_QueryVecPath_AllowsInBase(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "vec-ok-" + filepath.Base(dir)
	store.EnsureDirs(scope)

	// Plant an article + its vector so a legit in-base query vector resolves.
	stubArticle(t, scope, "a1", "Article One", "sum", "# A\n\nbody")
	idx := vector.New()
	idx.Add("a1", []float32{0.5, 0.5})
	if err := store.SaveVectors(scope, idx); err != nil {
		t.Fatalf("save vec index: %v", err)
	}

	// A query vector file inside the scope dir (under the kb base) is allowed.
	inBase := filepath.Join(store.ScopeDir(scope), "qvec.json")
	if err := os.WriteFile(inBase, []byte(`[0.5,0.5]`), 0o644); err != nil {
		t.Fatalf("write in-base vec: %v", err)
	}

	args := map[string]any{
		"scope":          scope,
		"query_vec_path": inBase,
	}
	if _, err := mcpSearch(newArticleCache(), args, scope); err != nil {
		t.Fatalf("mcpSearch rejected an in-base query_vec_path: %v", err)
	}
}

// stubArticle plants a minimal WikiArticle with the given id so that
// store.AttachVector's existence check passes and search can resolve hits.
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
	if err := store.SaveArticle(scope, a); err != nil {
		t.Fatalf("saveArticle %s: %v", id, err)
	}
}
