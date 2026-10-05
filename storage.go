// On-disk storage under the scope directory: base paths and scope resolution
// ("*", "a,b", single), raw docs, wiki articles (markdown + JSON frontmatter),
// the article-ID registry that keeps same-title articles from overwriting each
// other, the knowledge index, and the content-hash build cache.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// --- Storage ---

func basePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultBaseDir)
}

func scopeDir(scope string) string {
	safe := sanitize(scope)
	return filepath.Join(basePath(), safe)
}

var sanitizeRe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitize(s string) string {
	return sanitizeRe.ReplaceAllString(s, "_")
}

func ensureDirs(scope string) {
	root := scopeDir(scope)
	os.MkdirAll(filepath.Join(root, "raw"), 0o755)
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.MkdirAll(filepath.Join(root, "cache"), 0o755)
}

// --- Raw Doc Storage ---

func saveRawDoc(scope string, doc *model.RawDoc) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "raw", doc.ID+".json")
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadRawDoc(scope, id string) (*model.RawDoc, error) {
	path := filepath.Join(scopeDir(scope), "raw", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc model.RawDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// --- Article Storage ---

func saveArticle(scope string, a *model.WikiArticle) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "wiki", a.ID+".md")

	fm := model.Frontmatter{
		Title:        a.Title,
		Summary:      a.Summary,
		Concepts:     a.Concepts,
		Categories:   a.Categories,
		SourcePath:   a.SourcePath,
		SourceDocs:   a.SourceDocs,
		Backlinks:    a.Backlinks,
		WordCount:    a.WordCount,
		CompiledAt:   a.CompiledAt,
		CompiledWith: a.CompiledWith,
		Version:      a.Version,
		Audience:     a.Audience,
		Depth:        a.Depth,
		TargetWords:  a.TargetWords,
		Kind:         a.Kind,
		Term:         a.Term,
		Aliases:      a.Aliases,
		Category:     a.Category,
		Related:      a.Related,
		Usage:        a.Usage,
	}
	fmData, err := json.MarshalIndent(fm, "", "  ")
	if err != nil {
		return err
	}

	content := fmt.Sprintf("---\n%s\n---\n\n%s", string(fmData), a.Content)
	return os.WriteFile(path, []byte(content), 0o644)
}

// containedID rejects article ids that could escape the scope's wiki dir once
// joined and cleaned by filepath.Join (issue #23). Every id kb-go generates is
// slug-like — slugify() emits only [a-z0-9-], contentHash emits hex, and
// glossary term ids are slugs — so none legitimately carry a path separator or
// "..". The CLI (`kb show`, `kb recompile`) and the MCP surface (`kb_show`,
// `kb_glossary`, and any path that resolves a stored id) all funnel through
// loadArticle, so guarding here closes traversal for every caller in one place.
func containedID(id string) error {
	if id == "" {
		return fmt.Errorf("article id is empty")
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid article id %q: must not contain path separators or %q", id, "..")
	}
	return nil
}

// --- Article Identity ---

//
// An article's identity is its SOURCE, not its title. Ids start as a slug of
// the (LLM-chosen) title or file name, but every write path resolves the final
// id through idRegistry.claim so that:
//   - a source that already has an article keeps that article's id (re-compiling
//     under a new title replaces it in place, no stale twin), and any older
//     twins the same source left behind are retired;
//   - a slug already held by a DIFFERENT source is disambiguated
//     deterministically (slug + "-" + hex of the source path), so two sources
//     that happen to share a title never overwrite each other;
//   - an article with no source keeps the legacy slug-only behavior.
// The common no-collision case keeps the plain slug, so existing ids stay put.
//
// "Same source" means the same SourcePath for build/accept, where it is a path
// inside the scanned tree and unique per file. The ingest paths take a
// caller-chosen label ("manual", an upload's filename), which is not unique, so
// there a source match only replaces when the raw doc matches too; a label +
// slug match still overwrites in place, as it always has.

type articleIDEntry struct {
	id      string
	rawDocs []string
}

// idRegistry is a scope's id registry, built once per write command from the
// articles on disk and updated as the command claims ids, so same-batch
// collisions are seen too.
type idRegistry struct {
	owner    map[string]string // lower(id) -> SourcePath of the article holding it
	version  map[string]int    // id -> current version
	bySource map[string][]articleIDEntry
	retired  []string // stale twins to remove (see retire)
}

func loadIDRegistry(scope string) *idRegistry {
	r := &idRegistry{owner: map[string]string{}, version: map[string]int{}, bySource: map[string][]articleIDEntry{}}
	articles, _ := listArticles(scope)
	for _, a := range articles {
		r.record(a.ID, a.SourcePath, a.SourceDocs, a.Version)
	}
	return r
}

func (r *idRegistry) record(id, source string, rawDocs []string, version int) {
	r.owner[strings.ToLower(id)] = source
	r.version[id] = version
	if source == "" {
		return
	}
	entries := r.bySource[source]
	for i := range entries {
		if entries[i].id == id {
			entries[i].rawDocs = rawDocs
			return
		}
	}
	r.bySource[source] = append(entries, articleIDEntry{id: id, rawDocs: rawDocs})
}

// claim resolves the id for a write of source's article, given the slug its
// title or file name proposes, and records the claim. labelSource marks the
// ingest paths (see the section comment). Returns the id and the version to
// write. Stale twins of the same source are queued for retire.
func (r *idRegistry) claim(proposed, source string, rawDocs []string, labelSource bool) (string, int) {
	if source != "" {
		var same []string
		for _, e := range r.bySource[source] {
			if !labelSource || sharesRaw(e.rawDocs, rawDocs) {
				same = append(same, e.id)
			}
		}
		if len(same) > 0 {
			keep := same[0]
			for _, id := range same {
				if id == proposed || (keep != proposed && id < keep) {
					keep = id
				}
			}
			r.retireTwins(source, keep, same)
			v := r.version[keep] + 1
			r.record(keep, source, rawDocs, v)
			return keep, v
		}
	}
	id := proposed
	if source != "" {
		for _, n := range []int{8, 16, 64} {
			if owner, taken := r.owner[strings.ToLower(id)]; !taken || owner == source {
				break
			}
			id = disambiguatedID(proposed, source, n)
		}
	}
	v := r.version[id] + 1
	r.record(id, source, rawDocs, v)
	return id, v
}

// claimFixed records an article whose id is explicit (a glossary entry's
// frontmatter id) and retires any other article the same source left behind.
// Returns the version to write.
func (r *idRegistry) claimFixed(id, source string, rawDocs []string) int {
	if source != "" {
		var same []string
		for _, e := range r.bySource[source] {
			same = append(same, e.id)
		}
		r.retireTwins(source, id, same)
	}
	v := r.version[id] + 1
	r.record(id, source, rawDocs, v)
	return v
}

// retireTwins queues every id in ids except keep for removal. The retired ids
// stay owned by source for the rest of the command, so no other article can
// claim one before retire deletes its file.
func (r *idRegistry) retireTwins(source, keep string, ids []string) {
	kept := r.bySource[source][:0]
	for _, e := range r.bySource[source] {
		if e.id == keep || !slices.Contains(ids, e.id) {
			kept = append(kept, e)
		} else {
			r.retired = append(r.retired, e.id)
		}
	}
	r.bySource[source] = kept
}

// retire deletes the queued stale twins: their wiki files and vector entries.
// Callers rebuild the concept + search indexes from disk afterwards, which
// drops them there too. Raw docs are kept: a twin usually shares its raw doc
// with the article that replaced it.
func (r *idRegistry) retire(scope string) {
	if len(r.retired) == 0 {
		return
	}
	for _, id := range r.retired {
		if containedID(id) != nil {
			continue
		}
		p := filepath.Join(scopeDir(scope), "wiki", id+".md")
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove superseded article %s: %v\n", id, err)
		}
	}
	if vidx, err := loadOrCreateVectorIndex(scope); err == nil {
		removed := false
		for _, id := range r.retired {
			removed = vidx.Remove(id) || removed
		}
		if removed {
			if err := saveVectorIndex(scope, vidx); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to update vector index: %v\n", err)
			}
		}
	}
	r.retired = nil
}

// disambiguatedID appends n hex chars of the source path's hash to slug,
// trimming the slug so the id stays within slugify's 80-char cap.
func disambiguatedID(slug, source string, n int) string {
	if max := 80 - 1 - n; len(slug) > max {
		slug = strings.TrimRight(slug[:max], "-")
	}
	return slug + "-" + textutil.ContentHash(source)[:n]
}

func sharesRaw(a, b []string) bool {
	for _, x := range a {
		if x != "" && slices.Contains(b, x) {
			return true
		}
	}
	return false
}

// --- Article Storage ---

func loadArticle(scope, id string) (*model.WikiArticle, error) {
	if err := containedID(id); err != nil {
		return nil, err
	}
	path := filepath.Join(scopeDir(scope), "wiki", id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseArticle(id, string(data))
}

// splitFrontmatter splits "---\n<json>\n---\n<body>" into the JSON and the
// body. The closing delimiter is the first line after the opening one that is
// exactly "---" (a trailing \r is tolerated). It must not be a substring
// search: JSON string values can hold "---" (a markdown rule or "|---|" table
// divider in a summary) but never a raw newline, so a line that is only "---"
// cannot occur inside the JSON. ok is false if text does not start with "---"
// or the frontmatter is never closed.
func splitFrontmatter(text string) (fm, body string, ok bool) {
	if !strings.HasPrefix(text, "---") {
		return "", "", false
	}
	rest := text[3:]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", "", false
	}
	for pos := nl + 1; pos < len(rest); {
		next := len(rest)
		if end := strings.IndexByte(rest[pos:], '\n'); end >= 0 {
			next = pos + end + 1
		}
		if strings.TrimRight(rest[pos:next], "\r\n") == "---" {
			return rest[:pos], rest[next:], true
		}
		pos = next
	}
	return "", "", false
}

func parseArticle(id, text string) (*model.WikiArticle, error) {
	if !strings.HasPrefix(text, "---") {
		return &model.WikiArticle{
			ID:        id,
			Title:     id,
			Content:   text,
			WordCount: textutil.WordCount(text),
			Version:   1,
		}, nil
	}

	fmText, body, ok := splitFrontmatter(text)
	if !ok {
		return &model.WikiArticle{
			ID:        id,
			Title:     id,
			Content:   text,
			WordCount: textutil.WordCount(text),
			Version:   1,
		}, nil
	}

	var fm model.Frontmatter
	if err := json.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, fmt.Errorf("bad frontmatter in %s: %w", id, err)
	}

	// Apply defaults for terse-mode fields so legacy articles load cleanly.
	audience := fm.Audience
	if audience == "" {
		audience = "human"
	}
	depth := fm.Depth
	if depth == "" {
		depth = "deep"
	}
	targetWords := fm.TargetWords
	if targetWords == 0 {
		targetWords = 500
	}

	content := strings.TrimSpace(body)
	return &model.WikiArticle{
		ID:           id,
		Title:        fm.Title,
		Summary:      fm.Summary,
		Content:      content,
		Concepts:     textutil.NilToEmpty(fm.Concepts),
		Categories:   textutil.NilToEmpty(fm.Categories),
		SourcePath:   fm.SourcePath,
		SourceDocs:   textutil.NilToEmpty(fm.SourceDocs),
		Backlinks:    textutil.NilToEmpty(fm.Backlinks),
		WordCount:    fm.WordCount,
		CompiledAt:   fm.CompiledAt,
		CompiledWith: fm.CompiledWith,
		Version:      fm.Version,
		Audience:     audience,
		Depth:        depth,
		TargetWords:  targetWords,
		Kind:         fm.Kind,
		Term:         fm.Term,
		Aliases:      fm.Aliases,
		Category:     fm.Category,
		Related:      fm.Related,
		Usage:        fm.Usage,
	}, nil
}

func listArticles(scope string) ([]*model.WikiArticle, error) {
	dir := filepath.Join(scopeDir(scope), "wiki")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var articles []*model.WikiArticle
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		a, err := parseArticle(id, string(data))
		if err != nil {
			// stderr only: stdout carries JSON / MCP output.
			fmt.Fprintf(os.Stderr, "warning: skipping article %s: %v\n", id, err)
			continue
		}
		articles = append(articles, a)
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].ID < articles[j].ID })
	return articles, nil
}

// --- Index Storage ---

func loadIndex(scope string) *model.KnowledgeIndex {
	path := filepath.Join(scopeDir(scope), "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &model.KnowledgeIndex{
			Scope:    scope,
			Articles: map[string]any{},
			Concepts: map[string]*model.Concept{},
		}
	}
	var idx model.KnowledgeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return &model.KnowledgeIndex{
			Scope:    scope,
			Articles: map[string]any{},
			Concepts: map[string]*model.Concept{},
		}
	}
	if idx.Articles == nil {
		idx.Articles = map[string]any{}
	}
	if idx.Concepts == nil {
		idx.Concepts = map[string]*model.Concept{}
	}
	return &idx
}

func saveIndex(scope string, idx *model.KnowledgeIndex) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "index.json")
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func rebuildIndex(scope string, articles []*model.WikiArticle) *model.KnowledgeIndex {
	idx := &model.KnowledgeIndex{
		Scope:    scope,
		Articles: map[string]any{},
		Concepts: map[string]*model.Concept{},
	}
	catSet := map[string]bool{}

	for _, a := range articles {
		idx.Articles[a.ID] = map[string]any{
			"title":   a.Title,
			"summary": a.Summary,
		}

		for _, c := range a.Concepts {
			key := strings.ToLower(strings.TrimSpace(c))
			if key == "" {
				continue
			}
			concept, ok := idx.Concepts[key]
			if !ok {
				concept = &model.Concept{Name: c}
				idx.Concepts[key] = concept
			}
			if !slices.Contains(concept.Articles, a.ID) {
				concept.Articles = append(concept.Articles, a.ID)
			}
		}

		for _, cat := range a.Categories {
			catSet[cat] = true
		}
	}

	for cat := range catSet {
		idx.Categories = append(idx.Categories, cat)
	}
	sort.Strings(idx.Categories)

	return idx
}

// --- Cache ---

func loadCache(scope string) *model.Cache {
	path := filepath.Join(scopeDir(scope), "cache", "hashes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &model.Cache{Version: 1, Files: map[string]model.CacheEntry{}}
	}
	var c model.Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return &model.Cache{Version: 1, Files: map[string]model.CacheEntry{}}
	}
	if c.Files == nil {
		c.Files = map[string]model.CacheEntry{}
	}
	return &c
}

func saveCache(scope string, c *model.Cache) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "cache", "hashes.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// resolveScopes handles "*" (all scopes), "a,b,c" (multi), or single scope.
func resolveScopes(scope string) []string {
	if scope == "*" {
		// List all scope directories under basePath
		entries, err := os.ReadDir(basePath())
		if err != nil {
			return nil
		}
		var scopes []string
		for _, e := range entries {
			if e.IsDir() {
				// Check it has a wiki/ dir (is a real scope)
				wikiDir := filepath.Join(basePath(), e.Name(), "wiki")
				if info, err := os.Stat(wikiDir); err == nil && info.IsDir() {
					scopes = append(scopes, e.Name())
				}
			}
		}
		return scopes
	}
	if strings.Contains(scope, ",") {
		parts := strings.Split(scope, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return []string{scope}
}

const (
	defaultBaseDir = ".knowledge-base"
)
