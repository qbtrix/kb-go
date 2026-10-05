// Binary persistence for Index (vectors.bin, format version 1), the format
// kb writes; the JSON Save/Load in vector.go stay for reading legacy files.
//
// Layout (integers little-endian):
//
//	header (24 bytes)
//	  [0:4]   magic "KBVX"
//	  [4:8]   u32 version (1)
//	  [8:12]  u32 CRC-32C (Castagnoli) of every byte after the header
//	  [12:16] u32 dim: the common row length, 0 when rows differ (Add accepts any)
//	  [16:20] u32 count
//	  [20:24] u32 idTableLen
//	id table, per row in Entries order: uvarint idLen, id bytes, uvarint rowLen
//	norms:  count x f64, each row's sum of squares (float64, index order)
//	matrix: the rows' float32 values back to back
//
// Rows are stored exactly as given, NOT normalized: search must return the
// same float32 cosine bits as vector.Cosine (scores are printed, and ranks
// feed RRF), and a normalized row cannot reproduce them. The stored norm is
// the very sum Cosine accumulates for that row, so a search costs one dot
// product per row and no score changes.

package vector

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
)

const (
	binMagic     = "KBVX"
	binVersion   = 1
	binHeaderLen = 24
)

var (
	binCRC   = crc32.MakeTable(crc32.Castagnoli)
	errShape = errors.New("vector index: malformed binary file")
)

// sumSquares is the normB accumulation of Cosine for one row.
func sumSquares(v []float32) float64 {
	var n float64
	for _, x := range v {
		xf := float64(x)
		n += xf * xf
	}
	return n
}

// MarshalBinary encodes the index in the vectors.bin format.
func (idx *Index) MarshalBinary() ([]byte, error) {
	count := len(idx.Entries)
	dim := -1
	var ids []byte
	floats := 0
	for _, e := range idx.Entries {
		ids = binary.AppendUvarint(ids, uint64(len(e.ID)))
		ids = append(ids, e.ID...)
		ids = binary.AppendUvarint(ids, uint64(len(e.Vector)))
		floats += len(e.Vector)
		switch {
		case dim == -1:
			dim = len(e.Vector)
		case dim != len(e.Vector):
			dim = 0
		}
	}
	if dim < 0 {
		dim = 0
	}
	if count > math.MaxUint32 || len(ids) > math.MaxUint32 || dim > math.MaxUint32 {
		return nil, errors.New("vector index: too large for the binary format")
	}
	out := make([]byte, binHeaderLen, binHeaderLen+len(ids)+8*count+4*floats)
	copy(out, binMagic)
	binary.LittleEndian.PutUint32(out[4:], binVersion)
	binary.LittleEndian.PutUint32(out[12:], uint32(dim))
	binary.LittleEndian.PutUint32(out[16:], uint32(count))
	binary.LittleEndian.PutUint32(out[20:], uint32(len(ids)))
	out = append(out, ids...)
	for _, e := range idx.Entries {
		out = binary.LittleEndian.AppendUint64(out, math.Float64bits(sumSquares(e.Vector)))
	}
	for _, e := range idx.Entries {
		for _, x := range e.Vector {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(x))
		}
	}
	binary.LittleEndian.PutUint32(out[8:], crc32.Checksum(out[binHeaderLen:], binCRC))
	return out, nil
}

// DecodeBinary parses a vectors.bin image. Every row shares one backing
// array; any malformation (magic, version, CRC, lengths) is an error.
func DecodeBinary(data []byte) (*Index, error) {
	if len(data) < binHeaderLen || string(data[:4]) != binMagic ||
		binary.LittleEndian.Uint32(data[4:]) != binVersion {
		return nil, errShape
	}
	if crc32.Checksum(data[binHeaderLen:], binCRC) != binary.LittleEndian.Uint32(data[8:]) {
		return nil, errShape
	}
	dim := int(binary.LittleEndian.Uint32(data[12:]))
	count := uint64(binary.LittleEndian.Uint32(data[16:]))
	idLen := uint64(binary.LittleEndian.Uint32(data[20:]))
	rest := uint64(len(data)) - binHeaderLen
	if idLen > rest || count > rest {
		return nil, errShape
	}
	ids := data[binHeaderLen : binHeaderLen+idLen]
	tail := data[binHeaderLen+idLen:]

	entries := make([]Entry, count)
	lens := make([]int, count)
	floats := uint64(0)
	for i := range entries {
		l, n := binary.Uvarint(ids)
		if n <= 0 || l > uint64(len(ids)-n) {
			return nil, errShape
		}
		entries[i].ID = string(ids[n : n+int(l)])
		ids = ids[n+int(l):]
		r, n := binary.Uvarint(ids)
		if n <= 0 || r > uint64(len(tail)) {
			return nil, errShape
		}
		ids = ids[n:]
		if dim != 0 && int(r) != dim {
			return nil, errShape
		}
		lens[i] = int(r)
		floats += r
		if floats > uint64(len(tail)) {
			return nil, errShape
		}
	}
	if len(ids) != 0 || uint64(len(tail)) != 8*count+4*floats {
		return nil, errShape
	}
	norms := make([]float64, count)
	for i := range norms {
		norms[i] = math.Float64frombits(binary.LittleEndian.Uint64(tail[8*i:]))
	}
	m := tail[8*count:]
	all := make([]float32, floats)
	for i := range all {
		all[i] = math.Float32frombits(binary.LittleEndian.Uint32(m[4*i:]))
	}
	off := 0
	for i := range entries {
		entries[i].Vector = all[off : off+lens[i] : off+lens[i]]
		off += lens[i]
	}
	return &Index{Entries: entries, norms: norms}, nil
}

// SaveBinary writes the index to path in the binary format, via a temp file
// and rename so a concurrent reader never sees a torn file (falling back to
// an in-place write where the rename is refused; the CRC catches a torn read).
func (idx *Index) SaveBinary(path string) error {
	data, err := idx.MarshalBinary()
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Chmod(0o644)
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr == nil && cerr == nil {
		if err := os.Rename(name, path); err == nil {
			return nil
		}
	}
	os.Remove(name)
	if werr != nil {
		return werr
	}
	return os.WriteFile(path, data, 0o644)
}

// LoadBinary reads a vectors.bin file. A missing file is returned as the
// os error (callers decide what "missing" means).
func LoadBinary(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBinary(data)
}
