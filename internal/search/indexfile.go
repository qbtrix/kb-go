// Binary codec for cache/search_index.bin (IndexVersion 5).
//
// Layout (integers little-endian; uvarint/varint are encoding/binary's):
//
//	header (36 bytes)
//	  [0:4]   magic "KBSI"
//	  [4:8]   u32 version (5)
//	  [8:12]  u32 CRC-32C (Castagnoli) of every byte after the header
//	  [12:16] u32 reserved (0)
//	  [16:20] u32 nDocs    [20:24] u32 nTerms
//	  [24:28] u32 docTableLen  [28:32] u32 termBytesLen  [32:36] u32 postingsLen
//	doc table, per doc in index order (ascending id):
//	  str id, uvarint docLen, stamp, u8 flags (bit 0: glossary),
//	  strs titleTokens, strs conceptTokens, strs categories,
//	  [glossary only] str termKey, strs aliasKeys
//	  then uvarint nIgnored and per ignored file: str id, stamp
//	  str = uvarint len + bytes; strs = uvarint count + str...;
//	  stamp = varint mtimeNanos, varint size (-1 unknown), str sha256 ("" unless racy)
//	term offsets: (nTerms+1) x u32 into term bytes (terms sorted ascending)
//	postings offsets: (nTerms+1) x u32 into the postings blob
//	term bytes, then postings blob: per term, (uvarint docIdx delta, uvarint tf)...
//
// Decoding reads the header and doc table only; a query term is found by
// binary search over the term offsets and only its postings are decoded.
// Anything malformed (bad magic/version/CRC/lengths, offsets out of range) is
// an error, which callers treat as "no index" and rebuild. Doc order is the
// builder's; only an ascending-id doc table can pass the freshness check.

package search

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
	"math"
	"os"
	"path/filepath"
	"sort"
)

const (
	indexMagic     = "KBSI"
	indexHeaderLen = 36
)

var (
	crcTable      = crc32.MakeTable(crc32.Castagnoli)
	errIndexShape = errors.New("search index: malformed file")
)

// indexFile is the undecoded part of a loaded index: the term dictionary and
// postings blob, kept as-is so a re-save (settle) copies them verbatim.
type indexFile struct {
	nDocs    int
	nTerms   int
	offsets  []byte // term offsets then postings offsets, 2*(nTerms+1) u32
	terms    string // concatenated sorted terms
	postBlob []byte
}

func (f *indexFile) termOff(i int) int { return int(binary.LittleEndian.Uint32(f.offsets[4*i:])) }
func (f *indexFile) postOff(i int) int {
	return int(binary.LittleEndian.Uint32(f.offsets[4*(f.nTerms+1+i):]))
}

// postings decodes term's list, or nil when the term is absent.
func (f *indexFile) postings(term string) []Posting {
	i := sort.Search(f.nTerms, func(k int) bool { return f.terms[f.termOff(k):f.termOff(k+1)] >= term })
	if i == f.nTerms || f.terms[f.termOff(i):f.termOff(i+1)] != term {
		return nil
	}
	b := f.postBlob[f.postOff(i):f.postOff(i+1)]
	out := make([]Posting, 0, len(b)/2)
	doc := -1
	for len(b) > 0 {
		delta, n := binary.Uvarint(b)
		if n <= 0 || delta > uint64(f.nDocs) {
			return nil
		}
		b = b[n:]
		tf, n := binary.Uvarint(b)
		if n <= 0 || tf == 0 || tf > math.MaxInt32 {
			return nil
		}
		b = b[n:]
		if doc < 0 {
			doc = int(delta)
		} else {
			if delta == 0 {
				return nil
			}
			doc += int(delta)
		}
		if doc >= f.nDocs {
			return nil
		}
		out = append(out, Posting{doc, int(tf)})
	}
	return out
}

func appendStr(b []byte, s string) []byte {
	b = binary.AppendUvarint(b, uint64(len(s)))
	return append(b, s...)
}

func appendStrs(b []byte, ss []string) []byte {
	b = binary.AppendUvarint(b, uint64(len(ss)))
	for _, s := range ss {
		b = appendStr(b, s)
	}
	return b
}

// at returns ss[i], or nil past the end (hand-built indexes may omit fields).
func at(ss [][]string, i int) []string {
	if i < len(ss) {
		return ss[i]
	}
	return nil
}

func appendStamp(b []byte, st docStamp) []byte {
	b = binary.AppendVarint(b, st.mtime)
	b = binary.AppendVarint(b, st.size)
	return appendStr(b, st.hash)
}

// encodeIndex serializes si. A decoded index re-uses its raw dictionary and
// postings; a built one encodes them from Postings.
func encodeIndex(si *Index) ([]byte, error) {
	n := len(si.DocIDs)
	var doc []byte
	for i := 0; i < n; i++ {
		doc = appendStr(doc, si.DocIDs[i])
		doc = binary.AppendUvarint(doc, uint64(si.DocLens[i]))
		st := unknownStamp
		if i < len(si.stamps) {
			st = si.stamps[i]
		}
		doc = appendStamp(doc, st)
		gloss := i < len(si.glossary) && si.glossary[i]
		var flags byte
		if gloss {
			flags = 1
		}
		doc = append(doc, flags)
		doc = appendStrs(doc, at(si.TitleTokens, i))
		doc = appendStrs(doc, at(si.ConceptTokens, i))
		doc = appendStrs(doc, at(si.categories, i))
		if gloss {
			doc = appendStr(doc, si.termKeys[i])
			doc = appendStrs(doc, si.aliasKeys[i])
		}
	}
	doc = binary.AppendUvarint(doc, uint64(len(si.ignored)))
	for _, ig := range si.ignored {
		doc = appendStr(doc, ig.id)
		doc = appendStamp(doc, ig.stamp)
	}

	var nTerms int
	var offsets, terms, post []byte
	if si.file != nil {
		nTerms, offsets, terms, post = si.file.nTerms, si.file.offsets, []byte(si.file.terms), si.file.postBlob
	} else {
		keys := make([]string, 0, len(si.Postings))
		for t := range si.Postings {
			keys = append(keys, t)
		}
		sort.Strings(keys)
		nTerms = len(keys)
		termOffs := make([]byte, 0, 4*(nTerms+1))
		postOffs := make([]byte, 0, 4*(nTerms+1))
		for _, t := range keys {
			termOffs = binary.LittleEndian.AppendUint32(termOffs, uint32(len(terms)))
			postOffs = binary.LittleEndian.AppendUint32(postOffs, uint32(len(post)))
			terms = append(terms, t...)
			prev := 0
			for k, p := range si.Postings[t] {
				d := p[0] - prev
				if k == 0 {
					d = p[0]
				}
				post = binary.AppendUvarint(post, uint64(d))
				post = binary.AppendUvarint(post, uint64(p[1]))
				prev = p[0]
			}
		}
		termOffs = binary.LittleEndian.AppendUint32(termOffs, uint32(len(terms)))
		postOffs = binary.LittleEndian.AppendUint32(postOffs, uint32(len(post)))
		offsets = append(termOffs, postOffs...)
	}
	for _, l := range []int{n, nTerms, len(doc), len(terms), len(post)} {
		if l > math.MaxUint32 {
			return nil, errors.New("search index: too large for the binary format")
		}
	}

	out := make([]byte, indexHeaderLen, indexHeaderLen+len(doc)+len(offsets)+len(terms)+len(post))
	copy(out, indexMagic)
	binary.LittleEndian.PutUint32(out[4:], IndexVersion)
	binary.LittleEndian.PutUint32(out[16:], uint32(n))
	binary.LittleEndian.PutUint32(out[20:], uint32(nTerms))
	binary.LittleEndian.PutUint32(out[24:], uint32(len(doc)))
	binary.LittleEndian.PutUint32(out[28:], uint32(len(terms)))
	binary.LittleEndian.PutUint32(out[32:], uint32(len(post)))
	out = append(out, doc...)
	out = append(out, offsets...)
	out = append(out, terms...)
	out = append(out, post...)
	binary.LittleEndian.PutUint32(out[8:], crc32.Checksum(out[indexHeaderLen:], crcTable))
	return out, nil
}

// docReader walks the doc table; any overrun sets bad.
type docReader struct {
	s   string
	bad bool
}

func (r *docReader) uvarint() uint64 {
	var x uint64
	var shift uint
	for i := 0; i < len(r.s) && i < binary.MaxVarintLen64; i++ {
		c := r.s[i]
		if c < 0x80 {
			r.s = r.s[i+1:]
			return x | uint64(c)<<shift
		}
		x |= uint64(c&0x7f) << shift
		shift += 7
	}
	r.bad = true
	r.s = ""
	return 0
}

func (r *docReader) varint() int64 {
	ux := r.uvarint()
	x := int64(ux >> 1)
	if ux&1 != 0 {
		x = ^x
	}
	return x
}

func (r *docReader) byte1() byte {
	if len(r.s) == 0 {
		r.bad = true
		return 0
	}
	c := r.s[0]
	r.s = r.s[1:]
	return c
}

func (r *docReader) str() string {
	l := r.uvarint()
	if l > uint64(len(r.s)) {
		r.bad, r.s = true, ""
		return ""
	}
	v := r.s[:l]
	r.s = r.s[l:]
	return v
}

func (r *docReader) strs() []string {
	c := r.uvarint()
	if c > uint64(len(r.s)) { // every string takes at least one byte
		r.bad, r.s = true, ""
		return nil
	}
	out := make([]string, c)
	for i := range out {
		out[i] = r.str()
	}
	return out
}

func (r *docReader) stamp() docStamp {
	return docStamp{mtime: r.varint(), size: r.varint(), hash: r.str()}
}

// decodeIndex parses the header and doc table of a current-version file and validates the
// dictionary offsets; postings stay encoded until a term is looked up.
func decodeIndex(data []byte) (*Index, error) {
	if len(data) < indexHeaderLen || string(data[:4]) != indexMagic {
		return nil, errIndexShape
	}
	u32 := func(off int) uint64 { return uint64(binary.LittleEndian.Uint32(data[off:])) }
	if u32(4) != IndexVersion {
		return nil, errIndexShape
	}
	nDocs, nTerms, docLen, termLen, postLen := u32(16), u32(20), u32(24), u32(28), u32(32)
	offLen := 8 * (nTerms + 1)
	if uint64(len(data)) != indexHeaderLen+docLen+offLen+termLen+postLen {
		return nil, errIndexShape
	}
	if uint64(crc32.Checksum(data[indexHeaderLen:], crcTable)) != u32(8) {
		return nil, errIndexShape
	}
	p := uint64(indexHeaderLen)
	docTable := data[p : p+docLen]
	p += docLen
	f := &indexFile{nDocs: int(nDocs), nTerms: int(nTerms), offsets: data[p : p+offLen]}
	p += offLen
	f.terms = string(data[p : p+termLen])
	p += termLen
	f.postBlob = data[p : p+postLen]
	for i, prevT, prevP := 0, 0, 0; i <= f.nTerms; i++ {
		t, q := f.termOff(i), f.postOff(i)
		if t < prevT || q < prevP || (i == 0 && (t != 0 || q != 0)) {
			return nil, errIndexShape
		}
		prevT, prevP = t, q
	}
	if f.termOff(f.nTerms) != int(termLen) || f.postOff(f.nTerms) != int(postLen) {
		return nil, errIndexShape
	}

	n := int(nDocs)
	if uint64(n) > docLen { // every doc takes several bytes
		return nil, errIndexShape
	}
	si := &Index{
		V:             IndexVersion,
		DocIDs:        make([]string, n),
		DocLens:       make([]int, n),
		TitleTokens:   make([][]string, n),
		ConceptTokens: make([][]string, n),
		glossary:      make([]bool, n),
		termKeys:      make([]string, n),
		aliasKeys:     make([][]string, n),
		categories:    make([][]string, n),
		stamps:        make([]docStamp, n),
		file:          f,
	}
	r := &docReader{s: string(docTable)}
	total := 0
	for i := 0; i < n && !r.bad; i++ {
		si.DocIDs[i] = r.str()
		dl := r.uvarint()
		if dl > math.MaxInt32 {
			return nil, errIndexShape
		}
		si.DocLens[i] = int(dl)
		total += int(dl)
		si.stamps[i] = r.stamp()
		flags := r.byte1()
		si.TitleTokens[i] = r.strs()
		si.ConceptTokens[i] = r.strs()
		si.categories[i] = r.strs()
		if flags&1 != 0 {
			si.glossary[i] = true
			si.termKeys[i] = r.str()
			si.aliasKeys[i] = r.strs()
		}
	}
	nIgn := r.uvarint()
	if nIgn > uint64(len(r.s)) {
		return nil, errIndexShape
	}
	si.ignored = make([]ignoredFile, nIgn)
	for i := range si.ignored {
		si.ignored[i] = ignoredFile{id: r.str(), stamp: r.stamp()}
		if i > 0 && si.ignored[i-1].id >= si.ignored[i].id {
			return nil, errIndexShape
		}
	}
	if r.bad || len(r.s) != 0 {
		return nil, errIndexShape
	}
	if n > 0 {
		si.AvgDL = float64(total) / float64(n)
	}
	return si, nil
}

// writeFileAtomic writes data to a temp file beside path and renames it over
// path, so a concurrent reader sees the old or the new file, never a torn one.
// If the rename is refused (Windows, path open elsewhere) it writes in place;
// a reader racing that write fails the CRC and rebuilds.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_ = tmp.Chmod(0o644) // CreateTemp makes 0600; the index was always 0644
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
