// Vector-index persistence for a scope: the per-scope vectors.bin (binary,
// vector/binary.go) and the legacy vectors.json, load-or-create and save, the
// vector count for stats, attaching an externally computed embedding to an
// existing article (`kb ingest --vec`), and the contained-path vector loader
// the MCP server uses (agent-supplied paths must stay inside the knowledge
// base, issue #23).
//
// vectors.json: a read that has to decode it (no vectors.bin, or one that does
// not account for this JSON) also writes vectors.bin, best-effort and silent,
// and KEEPS the JSON so an older kb (JSON only) still finds its vectors after
// a downgrade. Only a vector write (SaveVectors) removes the JSON.
//
// Staleness invariant: a v2 vectors.bin records the size and mtime of the
// vectors.json it was built from or superseded (zero when none); it is used
// only while the JSON is absent or still matches that stamp exactly. Any other
// JSON (an older kb rewrote it, a restored copy) is re-migrated. A v1 file has
// no stamp and wins unless the JSON is newer by mtime. A corrupt vectors.bin is
// an error whatever JSON sits beside it.

package store

import (
	"errors"
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

// LoadVectors returns the on-disk index for the scope: vectors.bin while it
// accounts for any vectors.json beside it, else the JSON (migrated to
// vectors.bin on the way), else a fresh empty index. Errors only on actual
// I/O / parse failures — a missing file is the expected first-write case. A
// corrupt vectors.bin is an error, never an empty index or a fall back to the
// JSON: the next write would otherwise drop every stored vector.
func LoadVectors(scope string) (*vector.Index, error) {
	binPath, jsonPath := VectorIndexPath(scope), legacyVectorPath(scope)
	binFI, serr := os.Stat(binPath) // before the read: the migration guard
	if serr != nil {
		binFI = nil
	}
	idx, src, err := vector.LoadBinarySource(binPath)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("%s: %w", filepath.Base(binPath), err)
	}
	jsonFI, jerr := os.Stat(jsonPath)
	if err == nil && (jerr != nil || binCurrent(src, binFI, jsonFI)) {
		return idx, nil
	}
	if jerr != nil {
		if os.IsNotExist(jerr) {
			return vector.New(), nil
		}
		return nil, jerr
	}
	// Stamped from the stat taken before this decode: a JSON rewritten in
	// between looks stale next time (re-migrated), never current.
	idx, err = loadLegacyJSON(jsonPath)
	if err != nil {
		return nil, err
	}
	g := migrateGuard{jsonPath: jsonPath, json: jsonFI, bin: binFI}
	if data, err := idx.MarshalBinarySource(vector.SourceOf(jsonFI)); err == nil {
		_ = writeMigration(binPath, data, g) // best-effort, like search-index healing
	}
	return idx, nil
}

// binCurrent reports whether a decoded vectors.bin accounts for the
// vectors.json beside it: the JSON is the one its stamp records, or, for a
// v1 file (src nil, no stamp), the JSON is not newer than it.
func binCurrent(src *vector.Source, binFI, jsonFI os.FileInfo) bool {
	if src != nil {
		return *src == vector.SourceOf(jsonFI)
	}
	return binFI != nil && !jsonFI.ModTime().After(binFI.ModTime())
}

// Test seams: the JSON decoder (counted by tests) and the migration write
// (failed on purpose where the OS ignores a read-only directory).
var (
	loadLegacyJSON = vector.Load
	writeMigration = publishMigration
)

// migrateGuard is what a read migration saw before decoding the JSON: the
// JSON's stat, and the vectors.bin it is replacing (nil when there was none).
type migrateGuard struct {
	jsonPath string
	json     os.FileInfo
	bin      os.FileInfo
}

// unchanged reports whether the JSON and vectors.bin are still as the guard
// saw them, i.e. no vector write or other migration has landed since.
func (g migrateGuard) unchanged(binPath string) bool {
	fi, err := os.Stat(g.jsonPath)
	if err != nil || vector.SourceOf(fi) != vector.SourceOf(g.json) {
		return false
	}
	fi, err = os.Stat(binPath)
	if g.bin == nil {
		return os.IsNotExist(err)
	}
	return err == nil && vector.SourceOf(fi) == vector.SourceOf(g.bin)
}

var errMigrationRaced = errors.New("vector migration superseded")

// publishMigration writes a read migration's vectors.bin through a temp file.
// It never writes in place (a concurrent reader must never see a torn file),
// drops the result when either file changed since the read (a vector write
// must not be overwritten by vectors decoded before it), and creates a
// missing vectors.bin with a hard link so concurrent first readers cannot
// clobber each other or a writer: the first link wins, the rest find it.
func publishMigration(path string, data []byte, g migrateGuard) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	_ = tmp.Chmod(0o644)
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	if !g.unchanged(path) {
		return errMigrationRaced
	}
	if g.bin == nil {
		err := os.Link(name, path)
		if err == nil || os.IsExist(err) {
			return err
		}
		// No hard links here (FAT, some network shares): rename, which
		// replaces, so recheck that nothing has appeared first.
		if _, err := os.Stat(path); err == nil {
			return errMigrationRaced
		}
	}
	return os.Rename(name, path)
}

// SaveVectors persists the vector index to ~/.knowledge-base/{scope}/vectors.bin
// and removes a legacy vectors.json (its vectors were loaded into idx).
// Creates the parent directory if missing (matches EnsureDirs idiom for raw/,
// wiki/).
func SaveVectors(scope string, idx *vector.Index) error {
	path, legacy := VectorIndexPath(scope), legacyVectorPath(scope)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Stamp the JSON this write supersedes, so if removing it fails the
	// leftover still loses to vectors.bin on every load.
	var src vector.Source
	if fi, err := os.Stat(legacy); err == nil {
		src = vector.SourceOf(fi)
	}
	if err := idx.SaveBinarySource(path, src); err != nil {
		return err
	}
	_ = os.Remove(legacy)
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
