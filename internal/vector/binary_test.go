// Tests for the binary vector index (vectors.bin): round trip with mixed row
// lengths and zero rows, rejection of every truncation and of corrupt bytes
// without panicking, search over a decoded index returning the exact float32
// cosine bits and order of Cosine, the normalized-dot-product equivalence the
// format was weighed against, and query/row dim mismatches staying silent
// zero-score misses as with the JSON index, the version-2 source stamp
// round-tripping, and version-1 files still decoding (with no stamp).

package vector

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"math/rand"
	"path/filepath"
	"sort"
	"testing"
)

func randomIndex(n, dim int, seed int64) *Index {
	r := rand.New(rand.NewSource(seed))
	idx := New()
	for i := 0; i < n; i++ {
		v := make([]float32, dim)
		for j := range v {
			v[j] = float32(r.NormFloat64())
		}
		idx.Add(fmt.Sprintf("doc-%03d", i), v)
	}
	return idx
}

func sameVectors(t *testing.T, got, want *Index) {
	t.Helper()
	if len(got.Entries) != len(want.Entries) {
		t.Fatalf("entries: got %d, want %d", len(got.Entries), len(want.Entries))
	}
	for i := range want.Entries {
		g, w := got.Entries[i], want.Entries[i]
		if g.ID != w.ID || len(g.Vector) != len(w.Vector) {
			t.Fatalf("entry %d: got %s/%d, want %s/%d", i, g.ID, len(g.Vector), w.ID, len(w.Vector))
		}
		for j := range w.Vector {
			if math.Float32bits(g.Vector[j]) != math.Float32bits(w.Vector[j]) {
				t.Fatalf("entry %d[%d]: %v != %v", i, j, g.Vector[j], w.Vector[j])
			}
		}
	}
}

func TestBinaryRoundTrip(t *testing.T) {
	idx := randomIndex(40, 16, 1)
	idx.Add("zero", make([]float32, 16))
	idx.Add("short", []float32{1, 2, 3}) // mixed dims: Add never rejected them
	idx.Add("ünïcode id", []float32{float32(math.Inf(1)), -0, 1e-38})
	path := filepath.Join(t.TempDir(), "vectors.bin")
	if err := idx.SaveBinary(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadBinary(path)
	if err != nil {
		t.Fatal(err)
	}
	sameVectors(t, got, idx)
	for i, e := range got.Entries {
		if math.Float64bits(got.norms[i]) != math.Float64bits(sumSquares(e.Vector)) {
			t.Fatalf("stored norm %d differs", i)
		}
	}
	// Mutating a decoded row slice must not bleed into its neighbour.
	got.Entries[0].Vector = append(got.Entries[0].Vector, 9)
	if got.Entries[1].Vector[0] != idx.Entries[1].Vector[0] {
		t.Fatal("rows share capacity")
	}
	empty, err := New().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if e, err := DecodeBinary(empty); err != nil || e.Len() != 0 {
		t.Fatalf("empty index round trip: %v", err)
	}
}

func TestBinaryCorruptRejected(t *testing.T) {
	data, err := randomIndex(12, 8, 2).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	for l := 0; l < len(data); l++ {
		if _, err := DecodeBinary(data[:l]); err == nil {
			t.Fatalf("truncation to %d bytes decoded", l)
		}
	}
	if _, err := DecodeBinary(append(append([]byte(nil), data...), 0)); err == nil {
		t.Fatal("trailing byte accepted")
	}
	for i := range data {
		b := append([]byte(nil), data...)
		b[i] ^= 0x41
		if _, err := DecodeBinary(b); err == nil {
			t.Fatalf("flip at byte %d accepted", i)
		}
		// Behind a recomputed CRC the structural checks must hold without panics.
		binary.LittleEndian.PutUint32(b[8:], crc32.Checksum(b[binCRCFrom:], binCRC))
		if idx, err := DecodeBinary(b); err == nil {
			idx.Search(make([]float32, 8), 5)
		}
	}
}

func TestBinarySourceStamp(t *testing.T) {
	idx := randomIndex(5, 4, 3)
	src := Source{Size: 3_600_123, ModTime: 1_790_000_000_123_456_700}
	path := filepath.Join(t.TempDir(), "vectors.bin")
	if err := idx.SaveBinarySource(path, src); err != nil {
		t.Fatal(err)
	}
	got, stamp, err := LoadBinarySource(path)
	if err != nil || stamp == nil || *stamp != src {
		t.Fatalf("stamp: %+v err=%v", stamp, err)
	}
	sameVectors(t, got, idx)
	// MarshalBinary records no source (the zero stamp), still version 2.
	data, _ := idx.MarshalBinary()
	if _, stamp, err := DecodeBinarySource(data); err != nil || stamp == nil || *stamp != (Source{}) {
		t.Fatalf("zero stamp: %+v err=%v", stamp, err)
	}
}

func TestBinaryV1StillDecodes(t *testing.T) {
	idx := randomIndex(7, 6, 4)
	v2, _ := idx.MarshalBinary()
	if binary.LittleEndian.Uint32(v2[4:]) != 2 {
		t.Fatal("MarshalBinary should write version 2")
	}
	body := v2[binHeaderLen:]
	v1 := make([]byte, 24, 24+len(body))
	copy(v1, v2[:24])
	binary.LittleEndian.PutUint32(v1[4:], 1)
	v1 = append(v1, body...)
	binary.LittleEndian.PutUint32(v1[8:], crc32.Checksum(body, binCRC))
	got, stamp, err := DecodeBinarySource(v1)
	if err != nil || stamp != nil {
		t.Fatalf("v1: stamp=%v err=%v", stamp, err)
	}
	sameVectors(t, got, idx)
	for l := 0; l < len(v1); l++ {
		if _, err := DecodeBinary(v1[:l]); err == nil {
			t.Fatalf("v1 truncation to %d bytes decoded", l)
		}
	}
}

func TestDecodedSearchMatchesCosineExactly(t *testing.T) {
	idx := randomIndex(300, 384, 3)
	data, _ := idx.MarshalBinary()
	bin, err := DecodeBinary(data)
	if err != nil {
		t.Fatal(err)
	}
	r := rand.New(rand.NewSource(4))
	for q := 0; q < 50; q++ {
		query := make([]float32, 384)
		for j := range query {
			query[j] = float32(r.NormFloat64())
		}
		// Reference: the pre-binary search, Cosine per row.
		type sc struct {
			id string
			s  float32
		}
		var want []sc
		for _, e := range idx.Entries {
			if s := Cosine(query, e.Vector); s > 0 {
				want = append(want, sc{e.ID, s})
			}
		}
		sort.Slice(want, func(i, j int) bool { return want[i].s > want[j].s })
		for _, k := range []int{1, 10, 1000} {
			for name, got := range map[string][]Result{"binary": bin.Search(query, k), "json": idx.Search(query, k)} {
				if len(got) != min(k, len(want)) {
					t.Fatalf("%s k=%d: %d results, want %d", name, k, len(got), min(k, len(want)))
				}
				for i := range got {
					if got[i].ID != want[i].id || math.Float32bits(got[i].Score) != math.Float32bits(want[i].s) {
						t.Fatalf("%s q%d k=%d rank %d: %s %v, want %s %v", name, q, k, i, got[i].ID, got[i].Score, want[i].id, want[i].s)
					}
				}
			}
		}
		// The alternative format (rows normalized at write, search = dot
		// product) agrees with cosine to 1e-6 but not bit for bit, which is
		// why rows are stored as given with their norm instead.
		qn := math.Sqrt(sumSquares(query))
		for _, e := range idx.Entries {
			en := math.Sqrt(sumSquares(e.Vector))
			var dot float64
			for j := range query {
				dot += float64(query[j]) * float64(float32(float64(e.Vector[j])/en))
			}
			if d := math.Abs(dot/qn - float64(Cosine(query, e.Vector))); d > 1e-6 {
				t.Fatalf("normalized dot differs from cosine by %g", d)
			}
		}
	}
}

func TestDimMismatchStaysSilent(t *testing.T) {
	idx := New()
	idx.Add("three", []float32{1, 0, 0})
	idx.Add("two", []float32{1, 0})
	data, _ := idx.MarshalBinary()
	bin, err := DecodeBinary(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range []*Index{idx, bin} {
		if got := x.Search([]float32{1, 0, 0, 0}, 5); len(got) != 0 {
			t.Fatalf("4-d query matched rows of other dims: %v", got)
		}
		got := x.Search([]float32{1, 0}, 5)
		if len(got) != 1 || got[0].ID != "two" {
			t.Fatalf("2-d query: %v", got)
		}
	}
	// Add after decode invalidates the stored norms.
	bin.Add("two", []float32{0, 1})
	if got := bin.Search([]float32{1, 0}, 5); len(got) != 0 {
		t.Fatalf("stale norm after Add: %v", got)
	}
}

// BenchmarkLoad decodes a 379 x 1024 index (the real compiled bench scope's
// shape) from the legacy JSON file and from the binary file.
func BenchmarkLoad(b *testing.B) {
	idx := randomIndex(379, 1024, 6)
	dir := b.TempDir()
	jsonPath, binPath := filepath.Join(dir, "vectors.json"), filepath.Join(dir, "vectors.bin")
	if err := idx.Save(jsonPath); err != nil {
		b.Fatal(err)
	}
	if err := idx.SaveBinary(binPath); err != nil {
		b.Fatal(err)
	}
	b.Run("json", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := Load(jsonPath); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("binary", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			if _, err := LoadBinary(binPath); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkSearch is one query over 379 x 1024 rows: per-row norms computed
// (JSON-loaded) vs read from the binary file.
func BenchmarkSearch(b *testing.B) {
	idx := randomIndex(379, 1024, 7)
	data, _ := idx.MarshalBinary()
	bin, _ := DecodeBinary(data)
	q := randomIndex(1, 1024, 8).Entries[0].Vector
	b.Run("computed_norms", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			idx.norms = nil
			idx.Search(q, 40)
		}
	})
	b.Run("stored_norms", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bin.Search(q, 40)
		}
	})
}
