// Vector-index persistence for a scope: the per-scope vectors.bin (binary,
// vector/binary.go) with the legacy vectors.json read as a fallback and
// removed by the next save, load-or-create and save, the vector count for stats, attaching an
// externally computed embedding to an existing article (`kb ingest --vec`), and
// the contained-path vector loader the MCP server uses (agent-supplied paths
// must stay inside the knowledge base, issue #23).

package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qbtrix/kb-go/internal/vector"
)

// VectorIndexPath returns the on-disk location for a scope's vector index.
// Mirrors the storage layout used by raw/ and wiki/ — both are subdirs under
// ~/.knowledge-base/{scope}/, the vector index is a flat sibling file.
func VectorIndexPath(scope string) string {
	return filepath.Join(ScopeDir(scope), "vectors.bin")
}

// legacyVectorPath is the JSON vector index kb wrote before vectors.bin.
func legacyVectorPath(scope string) string {
	return filepath.Join(ScopeDir(scope), "vectors.json")
}

// LoadVectors returns the on-disk index for the scope: vectors.bin, else a
// legacy vectors.json, else a fresh empty index. Errors only on actual I/O /
// parse failures — a missing file is the expected first-write case. A corrupt
// vectors.bin is an error, never an empty index: the next write would
// otherwise drop every stored vector.
func LoadVectors(scope string) (*vector.Index, error) {
	idx, err := vector.LoadBinary(VectorIndexPath(scope))
	if err == nil {
		return idx, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", filepath.Base(VectorIndexPath(scope)), err)
	}
	path := legacyVectorPath(scope)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return vector.New(), nil
		}
		return nil, err
	}
	return vector.Load(path)
}

// SaveVectors persists the vector index to ~/.knowledge-base/{scope}/vectors.bin
// and removes a legacy vectors.json (the migration: its vectors were loaded
// into idx). Creates the parent directory if missing (matches EnsureDirs idiom
// for raw/, wiki/).
func SaveVectors(scope string, idx *vector.Index) error {
	path := VectorIndexPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := idx.SaveBinary(path); err != nil {
		return err
	}
	// Best-effort: vectors.bin wins over a leftover JSON on every load.
	_ = os.Remove(legacyVectorPath(scope))
	return nil
}

// LoadContainedVector is the agent-reachable variant of
// vector.LoadFile (issue #23). The MCP `kb_search` query_vec_path arg is
// agent-controlled over a persistent connection, so unlike the human-typed CLI
// `--query-vec`/`--vec` flags it must not read arbitrary disk paths. The query
// vector must resolve inside the kb base dir (~/.knowledge-base). On rejection
// the error names only the offending path, never the contained dir's contents.
func LoadContainedVector(path string) ([]float32, error) {
	if path == "" {
		return nil, fmt.Errorf("query vector path is empty")
	}
	base, err := filepath.Abs(BaseDir())
	if err != nil {
		return nil, fmt.Errorf("resolve base dir: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("invalid query vector path")
	}
	// filepath.Abs already cleans, collapsing any ".." so the prefix check is
	// sound. Require a path-segment boundary so "<base>-evil" can't slip past.
	if abs != base && !strings.HasPrefix(abs, base+string(filepath.Separator)) {
		return nil, fmt.Errorf("query vector path must be inside the knowledge base directory")
	}
	return vector.LoadFile(abs)
}

// AttachVector is the non-fatal core of `kb ingest --vec`. Validates
// inputs, loads the vector file, upserts into the per-scope vector index, and
// persists. Returns the resulting (dim, total-vectors-after) on success so
// the CLI wrapper can print a confirmation; tests call this directly to
// avoid the CLI's os.Exit-on-fatal flow.
//
// The article-existence check is intentional: if a caller mis-types the id,
// we want a hard error rather than a silent vector orphan. (Vectors keyed off
// non-existent ids would never be retrieved anyway, since search returns
// articles by id-lookup.)
func AttachVector(scope, articleID, vecPath string) (dim, total int, err error) {
	if articleID == "" {
		return 0, 0, fmt.Errorf("ingest --vec requires --id <article_id>")
	}
	if vecPath == "" {
		return 0, 0, fmt.Errorf("ingest --vec requires a vector file path")
	}
	// Confirm the article exists. Otherwise the vector would orphan and
	// hybrid search would skip it on rrfFuse's articlesByID lookup.
	if a, e := LoadArticle(scope, articleID); e != nil || a == nil {
		return 0, 0, fmt.Errorf("article %q not found in scope %q (run `kb ingest` to create it first)", articleID, scope)
	}
	vec, err := vector.LoadFile(vecPath)
	if err != nil {
		return 0, 0, fmt.Errorf("load vector: %w", err)
	}
	idx, err := LoadVectors(scope)
	if err != nil {
		return 0, 0, fmt.Errorf("load vector index: %w", err)
	}
	idx.Add(articleID, vec)
	if err := SaveVectors(scope, idx); err != nil {
		return 0, 0, fmt.Errorf("save vector index: %w", err)
	}
	return len(vec), idx.Len(), nil
}

// VectorCount returns how many entries the per-scope vector index holds.
// 0 when the index file doesn't exist yet. Used by cmdStats to populate the
// "vectors" field. Errors are swallowed and treated as 0 — stats is best-effort.
func VectorCount(scope string) int {
	idx, err := LoadVectors(scope)
	if err != nil {
		return 0
	}
	return idx.Len()
}
