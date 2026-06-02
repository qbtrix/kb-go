// contradiction.go — Cross-source definition contradiction detection (issue #19).
// Created: 2026-06-02
//
// Structural collisions (duplicate term, alias clash, dangling ref) are handled
// in glossary.go. This file adds SEMANTIC conflict detection: two or more sources
// that define the same term (or alias) but disagree on what it means. The build
// path used to dedupe these silently — last writer wins by article ID — which is
// exactly the failure mode the domain glossary (#15) exists to prevent.
//
// The detector is pure and offline: no LLM, no network. It groups candidates by a
// normalized term/alias key and flags a group when its definitions are
// "materially different" under the configured threshold. The tool FLAGS only; a
// human resolves which definition is canonical.
//
// Threshold (tunable via ContradictionConfig.Mode):
//   - "strict" (default): definitions conflict when their normalized first
//     sentence differs. Catches the common case (divergent opening line) while
//     ignoring sources that agree on the headline and only differ in detail.
//   - "loose": definitions conflict only when the full normalized body differs.
//     Fewer findings; use when first-sentence drift is expected/acceptable.
//
// An optional LLM-assisted similarity check can be layered on later as a flag;
// it is intentionally NOT a hard dependency here.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// ContradictionCandidate is one source's claim about a term. The build path
// produces these from glossary articles (including ones that would otherwise be
// silently overwritten by a same-ID save); glossaryValidate produces them from
// the on-disk glossary set.
type ContradictionCandidate struct {
	SourceID   string   // article id / source identifier
	Term       string   // canonical term this source defines
	Aliases    []string // alternative names this source claims for the term
	Definition string   // the definition text (article body or summary)
}

// ContradictionConfig tunes the "materially different" threshold. Mode defaults
// to "strict" when empty.
type ContradictionConfig struct {
	Mode string // "strict" (first-sentence) or "loose" (full-body)
}

// Contradiction is a flagged disagreement: one term, the conflicting source ids,
// and a definition snippet per source so a human can review without reopening
// every file.
type Contradiction struct {
	Term     string   `json:"term"`
	Sources  []string `json:"sources"`
	Snippets []string `json:"snippets"`
}

// detectContradictions groups candidates by normalized term/alias key and returns
// one Contradiction per key whose definitions are materially different under cfg.
// Pure and deterministic: results are sorted by term, snippets/sources follow the
// candidates' first-seen order within a key. No LLM, no I/O.
func detectContradictions(candidates []ContradictionCandidate, cfg ContradictionConfig) []Contradiction {
	mode := strings.ToLower(strings.TrimSpace(cfg.Mode))
	if mode == "" {
		mode = "strict"
	}

	// Group candidates by every name they claim (term + aliases). A source that
	// declares "Fabric" as a term collides with a source that declares "Fabric"
	// as an alias, so both feed the same group.
	type member struct {
		sourceID   string
		definition string
		defKey     string
	}
	groups := map[string][]member{}     // nameKey -> members
	displayTerm := map[string]string{}  // nameKey -> first-seen display term
	seenInKey := map[string]bool{}      // nameKey|sourceID -> already added

	addToGroup := func(nameKey, display string, m member) {
		dedupe := nameKey + "\x00" + m.sourceID
		if seenInKey[dedupe] {
			return
		}
		seenInKey[dedupe] = true
		groups[nameKey] = append(groups[nameKey], m)
		if _, ok := displayTerm[nameKey]; !ok {
			displayTerm[nameKey] = display
		}
	}

	for _, c := range candidates {
		names := append([]string{c.Term}, c.Aliases...)
		m := member{
			sourceID:   c.SourceID,
			definition: c.Definition,
			defKey:     definitionKey(c.Definition, mode),
		}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" {
				continue
			}
			key := strings.ToLower(n)
			addToGroup(key, c.Term, m)
		}
	}

	var out []Contradiction
	for key, members := range groups {
		if len(members) < 2 {
			continue
		}
		// Distinct definition keys mean the sources materially disagree.
		distinct := map[string]bool{}
		for _, m := range members {
			distinct[m.defKey] = true
		}
		if len(distinct) < 2 {
			continue // all sources agree under the threshold
		}
		term := displayTerm[key]
		if term == "" {
			term = key
		}
		c := Contradiction{Term: term}
		for _, m := range members {
			c.Sources = append(c.Sources, m.sourceID)
			c.Snippets = append(c.Snippets, snippet(m.definition))
		}
		out = append(out, c)
	}

	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Term) < strings.ToLower(out[j].Term)
	})
	return out
}

// definitionKey reduces a definition to its comparison key for the given mode.
// strict -> normalized first sentence; loose -> normalized full body.
func definitionKey(def, mode string) string {
	if mode == "loose" {
		return normalizeDef(def)
	}
	return normalizeDef(firstSentence(def))
}

// firstSentence returns the text up to (and including the position of) the first
// sentence terminator. Falls back to the whole string when no terminator exists.
func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, ".!?\n"); i >= 0 {
		return s[:i]
	}
	return s
}

// normalizeDef lowercases, collapses whitespace, and strips trailing punctuation
// so cosmetic differences (spacing, case, a trailing period) don't read as a
// contradiction.
func normalizeDef(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	return strings.TrimRight(s, " .!?;:,")
}

// snippet trims a definition to a single-line, length-capped form for findings.
func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	const max = 160
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

// formatContradictionIssue renders a Contradiction as a glossaryValidate issue
// line, matching the existing "duplicate term: ..." finding style.
func formatContradictionIssue(c Contradiction) string {
	pairs := make([]string, len(c.Sources))
	for i, src := range c.Sources {
		snip := ""
		if i < len(c.Snippets) {
			snip = c.Snippets[i]
		}
		pairs[i] = fmt.Sprintf("%s=%q", src, snip)
	}
	return fmt.Sprintf("contradiction: term %q defined differently across sources [%s]",
		c.Term, strings.Join(pairs, " vs "))
}

// candidatesFromArticles builds ContradictionCandidates from glossary articles,
// using the article body as the definition (falling back to Summary when the
// body is empty). Non-glossary articles are skipped.
func candidatesFromArticles(articles []*WikiArticle) []ContradictionCandidate {
	var cands []ContradictionCandidate
	for _, a := range articles {
		if a.Kind != "glossary" {
			continue
		}
		def := a.Content
		if strings.TrimSpace(def) == "" {
			def = a.Summary
		}
		id := a.ID
		if id == "" {
			id = a.Term
		}
		cands = append(cands, ContradictionCandidate{
			SourceID:   id,
			Term:       a.Term,
			Aliases:    a.Aliases,
			Definition: def,
		})
	}
	return cands
}
