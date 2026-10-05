// Domain glossary support (issue #15). Glossary articles are detected by source
// path (parent directory named "glossary"), skip compilation, and round-trip
// through the wiki verbatim: parseGlossarySource builds the WikiArticle straight
// from the file's JSON frontmatter. Kind="glossary" distinguishes them from
// module articles at search time (10x exact-Term/Alias boost in
// search.BM25WithIndex).
//
// glossaryList / glossaryShow / glossaryValidate are the programmatic API behind
// `kb glossary`. Validate also reports cross-source contradictions (two sources
// defining the same term differently) through detectContradictions.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// isGlossarySource returns true when relPath's parent directory is named
// exactly "glossary". Used by the build pipeline to skip LLM compilation for
// hand-curated glossary entries. Matches the test cases in glossary_test.go:
//
//	glossary/pocket.md           -> true
//	docs/glossary/soul.md        -> true
//	docs/wiki/glossary/ripple.md -> true
//	src/pocket.go                -> false
//	glossary.md                  -> false   (file named glossary, no parent dir)
//	glossaries/pocket.md         -> false   (plural, distinct dirname)
//	""                           -> false
func isGlossarySource(relPath string) bool {
	if relPath == "" {
		return false
	}
	parent := filepath.Base(filepath.Dir(relPath))
	return parent == "glossary"
}

// parseGlossarySource reads a hand-curated glossary .md file and constructs a
// WikiArticle directly from its frontmatter, preserving the body verbatim.
// No LLM is involved. Returns an error if frontmatter is missing or malformed.
//
// File shape (same JSON-between-fences format kb-go already uses elsewhere):
//
//	---
//	{ "id": "pocket", "title": "Pocket", "term": "Pocket", ... }
//	---
//
//	A Pocket is a workspace container.
//
// If the frontmatter omits "id", we derive it from the basename of relPath
// minus its extension (e.g. "pocket.md" -> "pocket").
func parseGlossarySource(raw []byte, relPath string) (*model.WikiArticle, error) {
	text := string(raw)
	if !strings.HasPrefix(text, "---") {
		return nil, fmt.Errorf("glossary source %s: missing frontmatter (expected leading '---')", relPath)
	}

	fmText, body, ok := store.SplitFrontmatter(text)
	if !ok {
		return nil, fmt.Errorf("glossary source %s: malformed frontmatter (need opening + closing '---')", relPath)
	}

	var fm model.Frontmatter
	if err := json.Unmarshal([]byte(fmText), &fm); err != nil {
		return nil, fmt.Errorf("glossary source %s: bad frontmatter JSON: %w", relPath, err)
	}

	// The "id" field is not on Frontmatter (it's the filename for module
	// articles). For glossary sources we accept an explicit "id" in the JSON
	// blob; fall back to basename(relPath) minus extension.
	var idHolder struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal([]byte(fmText), &idHolder)
	id := strings.TrimSpace(idHolder.ID)
	if id == "" {
		base := filepath.Base(relPath)
		id = strings.TrimSuffix(base, filepath.Ext(base))
	}

	title := fm.Title
	if title == "" {
		if fm.Term != "" {
			title = fm.Term
		} else {
			title = id
		}
	}

	// Body is everything after the closing '---'. Preserve it verbatim aside
	// from a leading newline trim — store.SaveArticle/store.ParseArticle round-trip uses
	// TrimSpace too, so we stay consistent. The marker assertion in test #4
	// only checks Contains, so internal bytes are what matter.
	content := strings.TrimSpace(body)

	return &model.WikiArticle{
		ID:           id,
		Title:        title,
		Summary:      fm.Summary,
		Content:      content,
		Concepts:     textutil.NilToEmpty(fm.Concepts),
		Categories:   textutil.NilToEmpty(fm.Categories),
		SourceDocs:   textutil.NilToEmpty(fm.SourceDocs),
		Backlinks:    textutil.NilToEmpty(fm.Backlinks),
		WordCount:    fm.WordCount,
		CompiledAt:   fm.CompiledAt,
		CompiledWith: fm.CompiledWith,
		Version:      fm.Version,
		Audience:     fm.Audience,
		Depth:        fm.Depth,
		TargetWords:  fm.TargetWords,
		Kind:         "glossary",
		Term:         fm.Term,
		Aliases:      fm.Aliases,
		Category:     fm.Category,
		Related:      fm.Related,
	}, nil
}

// --- CLI ---

// glossaryList writes a tab-aligned table of glossary entries to out. Returns
// nil with a "no entries" line if the scope contains no glossary articles.
func glossaryList(scope string, out io.Writer) error {
	articles, err := store.ListArticles(scope)
	if err != nil {
		return err
	}

	var entries []*model.WikiArticle
	for _, a := range articles {
		if a.Kind == "glossary" {
			entries = append(entries, a)
		}
	}

	if len(entries) == 0 {
		fmt.Fprintln(out, "No glossary entries found.")
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TERM\tCATEGORY\tALIASES\tFILE")
	for _, a := range entries {
		term := a.Term
		if term == "" {
			term = a.Title
		}
		cat := a.Category
		if cat == "" {
			cat = "-"
		}
		aliases := "-"
		if len(a.Aliases) > 0 {
			aliases = strings.Join(a.Aliases, ",")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s.md\n", term, cat, aliases, a.ID)
	}
	return tw.Flush()
}

// glossaryShow finds a glossary entry by Term or Alias (case-insensitive) and
// writes its body to out. Returns an error preserving the input term's casing
// if no match is found.
func glossaryShow(scope, term string, out io.Writer) error {
	articles, err := store.ListArticles(scope)
	if err != nil {
		return err
	}
	needle := strings.ToLower(strings.TrimSpace(term))
	for _, a := range articles {
		if a.Kind != "glossary" {
			continue
		}
		if strings.ToLower(a.Term) == needle {
			fmt.Fprint(out, a.Content)
			return nil
		}
		for _, al := range a.Aliases {
			if strings.ToLower(al) == needle {
				fmt.Fprint(out, a.Content)
				return nil
			}
		}
	}
	return fmt.Errorf("term %q not found in glossary", term)
}

// glossaryValidate inspects every glossary entry in the scope and returns a
// slice of human-readable issue strings. An empty slice + nil error means the
// glossary is clean. Iteration is in stable ID order so error messages are
// deterministic across runs.
//
// Checks performed:
//  1. Duplicate Term across two articles (case-insensitive)
//  2. Duplicate alias across two articles (case-insensitive)
//  3. Alias collides with another article's Term (case-insensitive)
//  4. Dangling Related reference (no Term or alias resolves it)
func glossaryValidate(scope string) ([]string, error) {
	articles, err := store.ListArticles(scope)
	if err != nil {
		return nil, err
	}

	// Filter glossary-only, stable-ordered (store.ListArticles already sorts by ID).
	var entries []*model.WikiArticle
	for _, a := range articles {
		if a.Kind == "glossary" {
			entries = append(entries, a)
		}
	}
	// Defensive re-sort in case store.ListArticles' ordering ever changes upstream.
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })

	var issues []string

	// Build a "term-lower -> articleID" map and "alias-lower -> []articleID".
	termOwner := map[string]string{} // termLower -> article ID
	for _, a := range entries {
		if a.Term == "" {
			continue
		}
		tl := strings.ToLower(a.Term)
		if prev, ok := termOwner[tl]; ok {
			issues = append(issues, fmt.Sprintf("duplicate term: %q in %s and %s", a.Term, prev, a.ID))
			continue
		}
		termOwner[tl] = a.ID
	}

	// Duplicate alias check: alias -> articles that declare it.
	aliasOwners := map[string][]struct {
		articleID string
		original  string
	}{}
	for _, a := range entries {
		seenInArticle := map[string]bool{}
		for _, al := range a.Aliases {
			if al == "" {
				continue
			}
			alLower := strings.ToLower(al)
			// De-dupe within the same article so a sloppy frontmatter doesn't
			// fire a "duplicate alias" on itself.
			if seenInArticle[alLower] {
				continue
			}
			seenInArticle[alLower] = true
			aliasOwners[alLower] = append(aliasOwners[alLower], struct {
				articleID string
				original  string
			}{a.ID, al})
		}
	}
	// Walk in deterministic order.
	aliasKeys := make([]string, 0, len(aliasOwners))
	for k := range aliasOwners {
		aliasKeys = append(aliasKeys, k)
	}
	sort.Strings(aliasKeys)
	for _, k := range aliasKeys {
		owners := aliasOwners[k]
		if len(owners) > 1 {
			// Report each subsequent collision against the first.
			first := owners[0]
			for _, dup := range owners[1:] {
				issues = append(issues, fmt.Sprintf("duplicate alias: %q in %s and %s", dup.original, first.articleID, dup.articleID))
			}
		}
	}

	// Alias <-> Term collisions (case-insensitive). An alias on article A
	// matches a Term on article B (B != A).
	for _, a := range entries {
		for _, al := range a.Aliases {
			if al == "" {
				continue
			}
			alLower := strings.ToLower(al)
			if owner, ok := termOwner[alLower]; ok && owner != a.ID {
				issues = append(issues, fmt.Sprintf("alias %q in %s collides with term in %s", al, a.ID, owner))
			}
		}
	}

	// Dangling Related references. A reference resolves if it matches any
	// known Term or any known alias (case-insensitive).
	knownNames := map[string]bool{}
	for k := range termOwner {
		knownNames[k] = true
	}
	for k := range aliasOwners {
		knownNames[k] = true
	}
	for _, a := range entries {
		for _, ref := range a.Related {
			if ref == "" {
				continue
			}
			if !knownNames[strings.ToLower(ref)] {
				issues = append(issues, fmt.Sprintf("related reference %q in %s not found", ref, a.ID))
			}
		}
	}

	// Cross-source semantic contradictions (issue #19): same term/alias defined
	// with materially different definitions across two or more sources. Strict
	// threshold by default — see contradiction.go. This is additive to the
	// structural checks above; a duplicate-term collision can also be a
	// contradiction, and both findings are reported.
	for _, c := range detectContradictions(candidatesFromArticles(entries), ContradictionConfig{Mode: "strict"}) {
		issues = append(issues, formatContradictionIssue(c))
	}

	return issues, nil
}
