// Category normalisation for `kb lint --normalize-categories`: clusters labels
// that differ only by casing, whitespace or trailing punctuation, picks a
// canonical variant per cluster, and rewrites articles to it on --apply.

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
)

// normalizeCategory reduces a category label to a canonical comparison key.
// Lowercase, whitespace-collapsed, trailing punctuation stripped. Used only
// for clustering variants together — the original casing is preserved in the
// cluster output, and the canonical form chosen for --apply is the most
// frequent *original* in the cluster, not this normalized key.
func normalizeCategory(c string) string {
	s := strings.ToLower(strings.TrimSpace(c))
	s = strings.Join(strings.Fields(s), " ")
	s = strings.TrimRight(s, ".,;:!?-_")
	return s
}

// categoryCluster groups originals that normalize to the same key and tracks
// per-variant article counts so we can pick the most popular form as canonical.
type categoryCluster struct {
	Key       string         `json:"key"`
	Variants  map[string]int `json:"variants"`  // original → article count
	Total     int            `json:"total"`     // sum of article counts
	Canonical string         `json:"canonical"` // chosen form for --apply
}

// pickCanonicalVariant chooses the representative form for a cluster.
// Order: highest article count → shortest original → already-normalized form
// (lowercase, no trailing punctuation) → alphabetical.
//
// The "already-normalized" tiebreak favors clean forms like "storage" over
// "Storage" when counts and lengths tie, which matches human intuition better
// than pure ASCII alpha (where capitals sort before lowercase).
func pickCanonicalVariant(variants map[string]int) string {
	type entry struct {
		name  string
		count int
	}
	entries := make([]entry, 0, len(variants))
	for k, v := range variants {
		entries = append(entries, entry{k, v})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		if len(entries[i].name) != len(entries[j].name) {
			return len(entries[i].name) < len(entries[j].name)
		}
		iClean := entries[i].name == normalizeCategory(entries[i].name)
		jClean := entries[j].name == normalizeCategory(entries[j].name)
		if iClean != jClean {
			return iClean
		}
		return entries[i].name < entries[j].name
	})
	return entries[0].name
}

// affectedArticleCount returns how many articles have at least one category
// that would be rewritten if --apply ran. Used for the dry-run summary.
func affectedArticleCount(articles []*model.WikiArticle, clusters []*categoryCluster) int {
	rewriteMap := buildRewriteMap(clusters)
	count := 0
	for _, a := range articles {
		for _, cat := range a.Categories {
			if canonical, ok := rewriteMap[cat]; ok && canonical != cat {
				count++
				break
			}
		}
	}
	return count
}

// buildRewriteMap flattens clusters into a flat original→canonical lookup.
// Originals that are already canonical map to themselves; non-canonical
// originals map to the chosen canonical.
func buildRewriteMap(clusters []*categoryCluster) map[string]string {
	m := map[string]string{}
	for _, c := range clusters {
		for variant := range c.Variants {
			m[variant] = c.Canonical
		}
	}
	return m
}

// applyCategoryCanonical rewrites each article's Categories to use canonical
// forms, saving only articles that actually changed. Returns the count of
// rewritten articles.
func applyCategoryCanonical(scope string, articles []*model.WikiArticle, clusters []*categoryCluster) int {
	rewriteMap := buildRewriteMap(clusters)
	changed := 0
	for _, a := range articles {
		dirty := false
		seen := map[string]bool{}
		newCats := make([]string, 0, len(a.Categories))
		for _, cat := range a.Categories {
			replacement, ok := rewriteMap[cat]
			if !ok {
				replacement = cat
			}
			if replacement != cat {
				dirty = true
			}
			// Dedupe — rewriting can collapse two variants into one slot.
			if seen[replacement] {
				dirty = true
				continue
			}
			seen[replacement] = true
			newCats = append(newCats, replacement)
		}
		if dirty {
			a.Categories = newCats
			if err := saveArticle(scope, a); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to save %s: %v\n", a.ID, err)
				continue
			}
			changed++
		}
	}
	// Rebuild and persist the BM25 index since categories flow into it.
	if changed > 0 {
		all, _ := listArticles(scope)
		idx := rebuildIndex(scope, all)
		if err := saveIndex(scope, idx); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to save index after category normalization: %v\n", err)
		}
	}
	return changed
}

// clusterCategories clusters the category labels of articles that differ
// only by casing/whitespace/punctuation. It returns the noisy clusters (more
// than one distinct variant, Canonical picked) sorted by total desc then key,
// and the number of normalized groups seen overall.
func clusterCategories(articles []*model.WikiArticle) (noisy []*categoryCluster, groups int) {
	// Build clusters: normalized key → map of original → article count
	clusters := map[string]*categoryCluster{}
	for _, a := range articles {
		for _, cat := range a.Categories {
			key := normalizeCategory(cat)
			if key == "" {
				continue
			}
			c, ok := clusters[key]
			if !ok {
				c = &categoryCluster{Key: key, Variants: map[string]int{}}
				clusters[key] = c
			}
			c.Variants[cat]++
			c.Total++
		}
	}

	// Keep only clusters with >1 distinct variant — those are the noisy ones.
	for _, c := range clusters {
		if len(c.Variants) > 1 {
			c.Canonical = pickCanonicalVariant(c.Variants)
			noisy = append(noisy, c)
		}
	}
	// Sort by total desc (most impactful clusters first), then key alpha.
	sort.Slice(noisy, func(i, j int) bool {
		if noisy[i].Total != noisy[j].Total {
			return noisy[i].Total > noisy[j].Total
		}
		return noisy[i].Key < noisy[j].Key
	})
	return noisy, len(clusters)
}
