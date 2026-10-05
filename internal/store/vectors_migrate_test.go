// Tests for the read-side vectors.json -> vectors.bin migration: a read of a
// JSON-only scope writes vectors.bin (keeping the JSON for older binaries),
// later reads skip the JSON, a vectors.json changed after migration (an older
// kb wrote it) is re-migrated, a failed migration write never fails the read,
// concurrent first reads leave one valid file, the write path still removes the
// JSON, a v1 vectors.bin is still honoured, and a corrupt vectors.bin stays an
// error.

package store

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/vector"
)

// countJSONDecodes counts vectors.json decodes for the rest of the test.
func countJSONDecodes(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	orig := loadLegacyJSON
	loadLegacyJSON = func(path string) (*vector.Index, error) {
		n.Add(1)
		return orig(path)
	}
	t.Cleanup(func() { loadLegacyJSON = orig })
	return &n
}

// legacyScope plants a JSON-only scope (what kb v0.4.0 and earlier wrote)
// and returns the scope and its vectors.json path.
func legacyScope(t *testing.T, name string, idx *vector.Index) (string, string) {
	t.Helper()
	_, scope := vectorTestEnv(t, name)
	jsonPath := filepath.Join(ScopeDir(scope), "vectors.json")
	if err := idx.Save(jsonPath); err != nil {
		t.Fatal(err)
	}
	return scope, jsonPath
}

func sampleIndex(rows int, seed float32) *vector.Index {
	idx := vector.New()
	for i := 0; i < rows; i++ {
		v := make([]float32, 8)
		for j := range v {
			v[j] = seed*float32(i+1) - float32(j)*0.37 + float32(i*j%5)
		}
		idx.Add("doc-"+string(rune('a'+i)), v)
	}
	return idx
}

func assertSameSearch(t *testing.T, want, got *vector.Index) {
	t.Helper()
	for _, q := range [][]float32{{1, 0, 0, 0, 0, 0, 0, 0}, {0.3, -1, 2, 0.5, 0, 1, -0.2, 4}} {
		w, g := want.Search(q, 10), got.Search(q, 10)
		if !reflect.DeepEqual(w, g) {
			t.Fatalf("results differ for %v:\nwant %v\n got %v", q, w, g)
		}
	}
}

func TestReadMigratesJSONToBinary(t *testing.T) {
	src := sampleIndex(6, 0.5)
	scope, jsonPath := legacyScope(t, "readmig", src)
	fromJSON, err := vector.Load(jsonPath)
	if err != nil {
		t.Fatal(err)
	}

	idx, err := LoadVectors(scope)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertSameSearch(t, fromJSON, idx)

	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("a read must keep vectors.json for older binaries: %v", err)
	}
	bin, err := vector.LoadBinary(VectorIndexPath(scope))
	if err != nil {
		t.Fatalf("read did not write a valid vectors.bin: %v", err)
	}
	if !reflect.DeepEqual(bin.Entries, fromJSON.Entries) {
		t.Fatal("migrated vectors.bin differs from vectors.json")
	}
	assertSameSearch(t, fromJSON, bin)
	assertNoTempFiles(t, scope)
}

func TestSecondReadUsesBinary(t *testing.T) {
	scope, jsonPath := legacyScope(t, "second", sampleIndex(4, 1))
	decodes := countJSONDecodes(t)

	first, err := LoadVectors(scope)
	if err != nil || decodes.Load() != 1 {
		t.Fatalf("first read: err=%v decodes=%d", err, decodes.Load())
	}
	second, err := LoadVectors(scope)
	if err != nil {
		t.Fatal(err)
	}
	if n := decodes.Load(); n != 1 {
		t.Fatalf("second read decoded vectors.json again (%d decodes)", n)
	}
	if VectorCount(scope) != 4 || decodes.Load() != 1 {
		t.Fatalf("VectorCount re-decoded the JSON (%d decodes)", decodes.Load())
	}
	assertSameSearch(t, first, second)
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatalf("vectors.json removed by a read: %v", err)
	}
}

func TestJSONChangedAfterMigrationIsRemigrated(t *testing.T) {
	scope, jsonPath := legacyScope(t, "remig", sampleIndex(3, 1))
	decodes := countJSONDecodes(t)
	if _, err := LoadVectors(scope); err != nil {
		t.Fatal(err)
	}

	// An older kb (downgrade) writes vectors.json; it never touches .bin.
	newer := sampleIndex(5, 2)
	if err := newer.Save(jsonPath); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(jsonPath, later, later)

	got, err := LoadVectors(scope)
	if err != nil {
		t.Fatal(err)
	}
	if decodes.Load() != 2 || got.Len() != 5 {
		t.Fatalf("newer vectors.json not re-migrated: decodes=%d len=%d", decodes.Load(), got.Len())
	}
	assertSameSearch(t, newer, got)
	bin, err := vector.LoadBinary(VectorIndexPath(scope))
	if err != nil || !reflect.DeepEqual(bin.Entries, newer.Entries) {
		t.Fatalf("vectors.bin not rewritten from the newer JSON: %v", err)
	}
	if _, err := LoadVectors(scope); err != nil || decodes.Load() != 2 {
		t.Fatalf("re-migrated .bin not used: err=%v decodes=%d", err, decodes.Load())
	}

	// A JSON swapped in with an OLDER mtime (a restored backup, a copy that
	// kept timestamps) still differs from the recorded stamp: re-migrated too.
	restored := sampleIndex(2, 3)
	if err := restored.Save(jsonPath); err != nil {
		t.Fatal(err)
	}
	earlier := time.Now().Add(-time.Hour)
	os.Chtimes(jsonPath, earlier, earlier)
	got, err = LoadVectors(scope)
	if err != nil || got.Len() != 2 || decodes.Load() != 3 {
		t.Fatalf("older-mtime JSON not re-migrated: err=%v len=%d decodes=%d", err, got.Len(), decodes.Load())
	}
	assertSameSearch(t, restored, got)
}

func TestUnwritableScopeStillReads(t *testing.T) {
	src := sampleIndex(4, 1.5)
	scope, _ := legacyScope(t, "rodir", src)
	dir := ScopeDir(scope)

	// A read-only scope dir where the OS enforces it (unix, non-root);
	// elsewhere (Windows ignores the directory read-only bit) inject the
	// same failure at the migration write.
	os.Chmod(dir, 0o555)
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	probe := filepath.Join(dir, "probe")
	if f, err := os.Create(probe); err == nil {
		f.Close()
		os.Remove(probe)
		orig := writeMigration
		writeMigration = func(string, []byte, migrateGuard) error { return errors.New("permission denied") }
		t.Cleanup(func() { writeMigration = orig })
	}

	stderr := captureStderr(t, func() {
		for i := 0; i < 2; i++ {
			idx, err := LoadVectors(scope)
			if err != nil {
				t.Fatalf("read failed when the migration could not be written: %v", err)
			}
			assertSameSearch(t, src, idx)
		}
	})
	if _, err := os.Stat(VectorIndexPath(scope)); !os.IsNotExist(err) {
		t.Fatalf("vectors.bin appeared in an unwritable dir (err=%v)", err)
	}
	if strings.Count(stderr, "\n") > 2 {
		t.Fatalf("migration failure was noisy on stderr: %q", stderr)
	}
}

func TestConcurrentFirstReads(t *testing.T) {
	src := sampleIndex(26, 0.75)
	scope, _ := legacyScope(t, "concurrent", src)

	const readers = 12
	var wg sync.WaitGroup
	errs := make([]error, readers)
	got := make([]*vector.Index, readers)
	start := make(chan struct{})
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			got[i], errs[i] = LoadVectors(scope)
		}(i)
	}
	close(start)
	wg.Wait()
	for i := range got {
		if errs[i] != nil {
			t.Fatalf("reader %d: %v", i, errs[i])
		}
		assertSameSearch(t, src, got[i])
	}
	bin, err := vector.LoadBinary(VectorIndexPath(scope))
	if err != nil || !reflect.DeepEqual(bin.Entries, src.Entries) {
		t.Fatalf("concurrent migration left an invalid vectors.bin: %v", err)
	}
	decodes := countJSONDecodes(t)
	if _, err := LoadVectors(scope); err != nil || decodes.Load() != 0 {
		t.Fatalf("vectors.bin from concurrent readers not used: err=%v decodes=%d", err, decodes.Load())
	}
	assertNoTempFiles(t, scope)
}

func TestWritePathRemovesJSONAfterReadMigration(t *testing.T) {
	src := vector.New()
	src.Add("a", []float32{0.25, -1.5, 3})
	scope, jsonPath := legacyScope(t, "writeafter", src)
	stubArticle(t, scope, "a", "A", "s", "body")
	stubArticle(t, scope, "b", "B", "s", "body")
	if _, err := LoadVectors(scope); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatal("read migration removed vectors.json")
	}

	vecPath := kbtest.WriteVecJSON(t, t.TempDir(), "vb.json", []float32{1, 2, 3}, "array")
	if _, total, err := AttachVector(scope, "b", vecPath); err != nil || total != 2 {
		t.Fatalf("attach: total=%d err=%v", total, err)
	}
	if _, err := os.Stat(jsonPath); !os.IsNotExist(err) {
		t.Fatalf("vector write left vectors.json behind (err=%v)", err)
	}
	idx, err := LoadVectors(scope)
	if err != nil || idx.Len() != 2 {
		t.Fatalf("after write: err=%v len=%d", err, idx.Len())
	}
}

// A vector write whose JSON removal fails still wins over that JSON: the .bin
// records the JSON it superseded, so the leftover never resurrects.
func TestWriteWinsOverUnremovedJSON(t *testing.T) {
	src := vector.New()
	src.Add("a", []float32{1, 2})
	scope, jsonPath := legacyScope(t, "unremoved", src)
	saved, _ := os.ReadFile(jsonPath)
	fi, _ := os.Stat(jsonPath)

	idx, _ := LoadVectors(scope)
	idx.Add("b", []float32{3, 4})
	if err := SaveVectors(scope, idx); err != nil {
		t.Fatal(err)
	}
	// Put the superseded JSON back exactly as it was (a failed remove).
	os.WriteFile(jsonPath, saved, 0o644)
	os.Chtimes(jsonPath, fi.ModTime(), fi.ModTime())

	got, err := LoadVectors(scope)
	if err != nil || got.Len() != 2 {
		t.Fatalf("leftover JSON resurrected over the write: err=%v len=%d", err, got.Len())
	}
}

// A v1 vectors.bin (no recorded source) is still read; with a vectors.json
// beside it the newer of the two by mtime wins.
func TestV1BinaryStillRead(t *testing.T) {
	_, scope := vectorTestEnv(t, "v1")
	binIdx := sampleIndex(3, 1)
	if err := os.WriteFile(VectorIndexPath(scope), marshalV1(t, binIdx), 0o644); err != nil {
		t.Fatal(err)
	}
	decodes := countJSONDecodes(t)
	got, err := LoadVectors(scope)
	if err != nil || !reflect.DeepEqual(got.Entries, binIdx.Entries) || decodes.Load() != 0 {
		t.Fatalf("v1 vectors.bin not read: err=%v decodes=%d", err, decodes.Load())
	}

	// An older leftover JSON loses to the v1 file.
	jsonPath := filepath.Join(ScopeDir(scope), "vectors.json")
	sampleIndex(1, 9).Save(jsonPath)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(jsonPath, old, old)
	if got, err := LoadVectors(scope); err != nil || got.Len() != 3 {
		t.Fatalf("older JSON beat a v1 vectors.bin: err=%v", err)
	}

	// A newer JSON (an older kb wrote it) is migrated.
	newer := sampleIndex(5, 4)
	newer.Save(jsonPath)
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(jsonPath, later, later)
	got, err = LoadVectors(scope)
	if err != nil || got.Len() != 5 {
		t.Fatalf("newer JSON beside a v1 vectors.bin not migrated: err=%v", err)
	}
	assertSameSearch(t, newer, got)
}

// A corrupt vectors.bin is an error even with a valid, newer vectors.json
// beside it (as #45 defines: the JSON never resurrects over a .bin), and a
// read leaves the corrupt file alone rather than overwriting it.
func TestCorruptBinaryWithValidJSONStaysAnError(t *testing.T) {
	scope, jsonPath := legacyScope(t, "corruptbin", sampleIndex(2, 1))
	bad := []byte("KBVX garbage")
	if err := os.WriteFile(VectorIndexPath(scope), bad, 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	os.Chtimes(jsonPath, later, later)

	if got, err := LoadVectors(scope); err == nil {
		t.Fatalf("corrupt vectors.bin loaded as %d entries", got.Len())
	}
	if after, _ := os.ReadFile(VectorIndexPath(scope)); string(after) != string(bad) {
		t.Fatal("a read overwrote the corrupt vectors.bin")
	}
	if _, err := os.Stat(jsonPath); err != nil {
		t.Fatal("vectors.json removed")
	}
}

// marshalV1 encodes idx in the version-1 vectors.bin layout (24-byte header,
// CRC over the body only), what the first binary-vector build wrote.
func marshalV1(t *testing.T, idx *vector.Index) []byte {
	t.Helper()
	v2, err := idx.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(v2[4:]) != 2 {
		t.Fatalf("MarshalBinary wrote version %d, want 2", binary.LittleEndian.Uint32(v2[4:]))
	}
	body := v2[40:]
	out := make([]byte, 24, 24+len(body))
	copy(out, v2[:24])
	binary.LittleEndian.PutUint32(out[4:], 1)
	out = append(out, body...)
	binary.LittleEndian.PutUint32(out[8:], crc32.Checksum(body, crc32.MakeTable(crc32.Castagnoli)))
	return out
}

func assertNoTempFiles(t *testing.T, scope string) {
	t.Helper()
	entries, _ := os.ReadDir(ScopeDir(scope))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

// captureStderr runs fn with os.Stderr redirected and returns what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	done := make(chan string)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			b.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- b.String()
	}()
	defer func() { os.Stderr = orig }()
	fn()
	w.Close()
	os.Stderr = orig
	return <-done
}
