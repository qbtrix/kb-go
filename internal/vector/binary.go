// Binary persistence for Index (vectors.bin, format version 2), the format
// kb writes; version-1 files are still read, and the JSON Save/Load in
// vector.go stay for reading legacy files.
//
// Layout (integers little-endian):
//
//	header (40 bytes; version 1 stops at byte 24)
//	  [0:4]   magic "KBVX"
//	  [4:8]   u32 version (2)
//	  [8:12]  u32 CRC-32C (Castagnoli) of every byte from 12 on (v1: from 24)
//	  [12:16] u32 dim: the common row length, 0 when rows differ (Add accepts any)
//	  [16:20] u32 count
//	  [20:24] u32 idTableLen
//	  [24:32] i64 source size:  the vectors.json this file was built from or
//	  [32:40] i64 source mtime: superseded (Source); both 0 when none
//	id table, per row in Entries order: uvarint idLen, id bytes, uvarint rowLen
//	norms:  count x f64, each row's sum of squares (float64, index order)
//	matrix: the rows' float32 values back to back
//
// Rows are stored exactly as given, NOT normalized: search must return the
// same float32 cosine bits as vector.Cosine (scores are printed, and ranks
// feed RRF), and a normalized row cannot reproduce them. The stored norm is
// the very sum Cosine accumulates for that row, so a search costs one dot
// product per row and no score changes.
//
// The source stamp lets the caller tell whether a vectors.json beside the
// file is the one it accounts for or a different one; this package only
// stores it.

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
	binMagic       = "KBVX"
	binVersion     = 2
	binHeaderLen   = 40
	binCRCFrom     = 12 // v2 checksums the rest of the header too
	binV1HeaderLen = 24
)

// Source identifies the vectors.json a vectors.bin was built from or
// superseded: its size and modification time (unix nanoseconds). The zero
// value records none.
type Source struct {
	Size    int64
	ModTime int64
}

// SourceOf stamps a file from its os.Stat result.
func SourceOf(fi os.FileInfo) Source {
	return Source{Size: fi.Size(), ModTime: fi.ModTime().UnixNano()}
}

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

// MarshalBinary encodes the index in the vectors.bin format, with no source.
func (idx *Index) MarshalBinary() ([]byte, error) {
	return idx.MarshalBinarySource(Source{})
}

// MarshalBinarySource encodes the index in the vectors.bin format, stamped
// with src.
func (idx *Index) MarshalBinarySource(src Source) ([]byte, error) {
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
	binary.LittleEndian.PutUint64(out[24:], uint64(src.Size))
	binary.LittleEndian.PutUint64(out[32:], uint64(src.ModTime))
	out = append(out, ids...)
	for _, e := range idx.Entries {
		out = binary.LittleEndian.AppendUint64(out, math.Float64bits(sumSquares(e.Vector)))
	}
	for _, e := range idx.Entries {
		for _, x := range e.Vector {
			out = binary.LittleEndian.AppendUint32(out, math.Float32bits(x))
		}
	}
	binary.LittleEndian.PutUint32(out[8:], crc32.Checksum(out[binCRCFrom:], binCRC))
	return out, nil
}

// DecodeBinary parses a vectors.bin image, dropping its source stamp.
func DecodeBinary(data []byte) (*Index, error) {
	idx, _, err := DecodeBinarySource(data)
	return idx, err
}

// DecodeBinarySource parses a vectors.bin image (version 1 or 2) and returns
// its source stamp, nil for a version-1 file (which has none). Every row
// shares one backing array; any malformation (magic, version, CRC, lengths)
// is an error.
func DecodeBinarySource(data []byte) (*Index, *Source, error) {
	if len(data) < binV1HeaderLen || string(data[:4]) != binMagic {
		return nil, nil, errShape
	}
	var src *Source
	hdr, crcFrom := binHeaderLen, binCRCFrom
	switch binary.LittleEndian.Uint32(data[4:]) {
	case 1:
		hdr, crcFrom = binV1HeaderLen, binV1HeaderLen
	case binVersion:
		if len(data) < binHeaderLen {
			return nil, nil, errShape
		}
		src = &Source{
			Size:    int64(binary.LittleEndian.Uint64(data[24:])),
			ModTime: int64(binary.LittleEndian.Uint64(data[32:])),
		}
	default:
		return nil, nil, errShape
	}
	if crc32.Checksum(data[crcFrom:], binCRC) != binary.LittleEndian.Uint32(data[8:]) {
		return nil, nil, errShape
	}
	idx, err := decodeBody(data, hdr)
	if err != nil {
		return nil, nil, err
	}
	return idx, src, nil
}

// decodeBody parses the id table, norms and matrix after a hdr-byte header.
func decodeBody(data []byte, hdr int) (*Index, error) {
	dim := int(binary.LittleEndian.Uint32(data[12:]))
	count := uint64(binary.LittleEndian.Uint32(data[16:]))
	idLen := uint64(binary.LittleEndian.Uint32(data[20:]))
	rest := uint64(len(data) - hdr)
	if idLen > rest || count > rest {
		return nil, errShape
	}
	ids := data[hdr : uint64(hdr)+idLen]
	tail := data[uint64(hdr)+idLen:]

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

// SaveBinary writes the index to path in the binary format with no source.
func (idx *Index) SaveBinary(path string) error {
	return idx.SaveBinarySource(path, Source{})
}

// SaveBinarySource writes the index to path in the binary format stamped
// with src, via a temp file and rename so a concurrent reader never sees a
// torn file (falling back to an in-place write where the rename is refused;
// the CRC catches a torn read).
func (idx *Index) SaveBinarySource(path string, src Source) error {
	data, err := idx.MarshalBinarySource(src)
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
	idx, _, err := LoadBinarySource(path)
	return idx, err
}

// LoadBinarySource is LoadBinary that also returns the source stamp (nil
// for a version-1 file).
func LoadBinarySource(path string) (*Index, *Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return DecodeBinarySource(data)
}
