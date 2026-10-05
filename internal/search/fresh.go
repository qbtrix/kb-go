// Freshness of the persisted search index against the scope's wiki/ files,
// checked with one ReadDir instead of reading every article.
//
// Each indexed doc carries its file's stamp (mtime in ns + size) and the index
// also stamps the wiki/*.md files store.ListArticles skips (unparseable), so
// an index is fresh only when wiki/ holds exactly those files with exactly
// those stamps. This catches added and removed files AND content rewritten
// under the same id (convo ingest, lint --normalize-categories --apply).
//
// Invariant (racy stamps): a file stamped less than racyWindow after its mtime
// could be overwritten within the same filesystem clock tick and keep both
// mtime and size, so its stamp also records the SHA-256 of its bytes and the
// check re-hashes it. Once such a file verifies at least racyWindow past its
// mtime, the hash is dropped (settle) and the index rewritten, so a scope built
// in one burst does not pay a hash per file on every later search. Stamps are
// always taken BEFORE the content they vouch for is read on the heal path, so
// a concurrent write can only make the index look stale, never fresh.

package search

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qbtrix/kb-go/internal/store"
)

// racyWindow matches the MCP article cache: generous enough for 2 s FAT
// mtimes.
const racyWindow = 3 * time.Second

// docStamp is one wiki file's identity when the index was built.
type docStamp struct {
	mtime int64  // ModTime().UnixNano()
	size  int64  // -1: unknown (file missing or unreadable when stamped); never fresh
	hash  string // SHA-256 of the bytes when racy at stamp time, else ""
}

// ignoredFile is a wiki/*.md file that store.ListArticles skips.
type ignoredFile struct {
	id    string
	stamp docStamp
}

var unknownStamp = docStamp{size: -1}

func wikiDir(scope string) string { return filepath.Join(store.ScopeDir(scope), "wiki") }

func hashFile(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	h := sha256.Sum256(data)
	return string(h[:]), true
}

// stampOf stamps one directory entry as of now (taken before the stat).
func stampOf(dir string, e os.DirEntry, now time.Time) docStamp {
	info, err := e.Info()
	if err != nil {
		return unknownStamp
	}
	st := docStamp{mtime: info.ModTime().UnixNano(), size: info.Size()}
	if now.Sub(info.ModTime()) < racyWindow {
		h, ok := hashFile(filepath.Join(dir, e.Name()))
		if !ok {
			return unknownStamp
		}
		st.hash = h
	}
	return st
}

// snapshotWiki stamps every wiki/*.md entry, keyed by article id.
func snapshotWiki(dir string, entries []os.DirEntry, now time.Time) map[string]docStamp {
	snap := make(map[string]docStamp, len(entries))
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".md"); ok {
			snap[id] = stampOf(dir, e, now)
		}
	}
	return snap
}

// assignStamps maps a pre-read snapshot onto the built doc table. Ids the
// snapshot lacks (added after it) get unknownStamp; snapshot files the listing
// skipped become ignored entries.
func assignStamps(snap map[string]docStamp, ids []string) ([]docStamp, []ignoredFile) {
	stamps := make([]docStamp, len(ids))
	indexed := make(map[string]bool, len(ids))
	for i, id := range ids {
		indexed[id] = true
		if st, ok := snap[id]; ok {
			stamps[i] = st
		} else {
			stamps[i] = unknownStamp
		}
	}
	var ignored []ignoredFile
	for id, st := range snap {
		if !indexed[id] {
			ignored = append(ignored, ignoredFile{id, st})
		}
	}
	sort.Slice(ignored, func(a, b int) bool { return ignored[a].id < ignored[b].id })
	return stamps, ignored
}

// stampDocs stamps the wiki files for an index built by a writer from an
// article listing it already holds. A file the index lacks is recorded as
// ignored only if ListArticles would skip it too (unreadable or unparseable);
// a parseable one stays unrecorded, so the next search sees the index as stale
// and rebuilds it.
func stampDocs(scope string, ids []string) ([]docStamp, []ignoredFile) {
	dir := wikiDir(scope)
	now := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		stamps := make([]docStamp, len(ids))
		for i := range stamps {
			stamps[i] = unknownStamp
		}
		return stamps, nil
	}
	snap := snapshotWiki(dir, entries, now)
	stamps, ignored := assignStamps(snap, ids)
	kept := ignored[:0]
	for _, ig := range ignored {
		data, err := os.ReadFile(filepath.Join(dir, ig.id+".md"))
		if err == nil {
			if _, perr := store.ParseArticle(ig.id, string(data)); perr == nil {
				continue // a real article missing from this index
			}
		}
		kept = append(kept, ig)
	}
	return stamps, kept
}

// sameStamp reports whether entry e still matches st, re-hashing racy stamps.
// settled is true when a racy stamp verified and is now past the window.
func sameStamp(dir string, e os.DirEntry, st docStamp, now time.Time) (same, settled bool) {
	if st.size < 0 {
		return false, false
	}
	info, err := e.Info()
	if err != nil || info.Size() != st.size || info.ModTime().UnixNano() != st.mtime {
		return false, false
	}
	if st.hash == "" {
		return true, false
	}
	h, ok := hashFile(filepath.Join(dir, e.Name()))
	if !ok || h != st.hash {
		return false, false
	}
	return true, now.Sub(info.ModTime()) >= racyWindow
}

// checkFresh reads wiki/ once and reports whether si still describes it.
func (si *Index) checkFresh(scope string) (fresh, settle bool) {
	now := time.Now()
	entries, err := os.ReadDir(wikiDir(scope))
	if err != nil {
		return false, false
	}
	return si.freshEntries(wikiDir(scope), entries, now)
}

// freshEntries is checkFresh over an existing ReadDir result; now must be
// taken before the ReadDir. settle reports racy stamps that can be dropped.
func (si *Index) freshEntries(dir string, entries []os.DirEntry, now time.Time) (fresh, settle bool) {
	if si == nil || si.V != IndexVersion || len(si.stamps) != len(si.DocIDs) {
		return false, false
	}
	if !sort.StringsAreSorted(si.DocIDs) {
		return false, false
	}
	seenDocs, seenIgnored := 0, 0
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".md")
		if !ok {
			continue
		}
		var st docStamp
		if i := sort.SearchStrings(si.DocIDs, id); i < len(si.DocIDs) && si.DocIDs[i] == id {
			st = si.stamps[i]
			seenDocs++
		} else {
			j := sort.Search(len(si.ignored), func(k int) bool { return si.ignored[k].id >= id })
			if j == len(si.ignored) || si.ignored[j].id != id {
				return false, false
			}
			st = si.ignored[j].stamp
			seenIgnored++
		}
		same, settled := sameStamp(dir, e, st, now)
		if !same {
			return false, false
		}
		settle = settle || settled
	}
	if seenDocs != len(si.DocIDs) || seenIgnored != len(si.ignored) {
		return false, false
	}
	return true, settle
}

// settleStamps drops the hash from racy stamps whose file mtime is now at
// least racyWindow old (call only right after freshEntries verified them).
func (si *Index) settleStamps(now time.Time) {
	settle := func(st *docStamp) {
		if st.hash != "" && now.Sub(time.Unix(0, st.mtime)) >= racyWindow {
			st.hash = ""
		}
	}
	for i := range si.stamps {
		settle(&si.stamps[i])
	}
	for i := range si.ignored {
		settle(&si.ignored[i].stamp)
	}
}
