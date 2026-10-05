// vectors_migrate_test.go — kb serve's kb_search reads vectors through
// store.LoadVectors like the CLI, so a hybrid search over a JSON-only scope
// writes vectors.bin and keeps vectors.json.

package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/vector"
)

func TestMCPHybridSearchMigratesVectorsJSON(t *testing.T) {
	dir := t.TempDir()
	kbtest.SetHome(t, dir)
	scope := "vec-mcpmig-" + filepath.Base(dir)
	store.EnsureDirs(scope)
	stubArticle(t, scope, "a1", "Article One", "sum", "# A\n\nbody about auth")

	legacy := vector.New()
	legacy.Add("a1", []float32{0.5, 0.5})
	jsonPath := filepath.Join(store.ScopeDir(scope), "vectors.json")
	if err := legacy.Save(jsonPath); err != nil {
		t.Fatal(err)
	}
	qvec := filepath.Join(store.ScopeDir(scope), "qvec.json")
	if err := os.WriteFile(qvec, []byte(`[0.5,0.5]`), 0o644); err != nil {
		t.Fatal(err)
	}

	args := map[string]any{"scope": scope, "query": "auth", "query_vec_path": qvec, "hybrid": true}
	res, err := mcpSearch(newArticleCache(), args, scope)
	if err != nil {
		t.Fatalf("hybrid kb_search: %v", err)
	}
	if hits, ok := res.([]map[string]any); ok && len(hits) == 0 {
		t.Fatal("no hits")
	}
	if _, err := vector.LoadBinary(store.VectorIndexPath(scope)); err != nil {
		t.Fatalf("kb_search did not migrate vectors.json: %v", err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("kb_search removed vectors.json: %v", err)
	}
}
