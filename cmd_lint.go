// Implements `kb lint`, plus the helpers only that command uses.

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

func cmdLint(args []string) {
	scope := flagStr(args, "--scope", "default")
	llmMode := flagBool(args, "--llm")
	normalizeCats := flagBool(args, "--normalize-categories")
	applyFix := flagBool(args, "--apply")
	jsonOut := flagBool(args, "--json")

	// --normalize-categories runs as a dedicated mode — it's a clustering
	// operation, not an issue-list, so it doesn't fit the LintIssue shape.
	if normalizeCats {
		runCategoryNormalize(scope, applyFix, jsonOut)
		return
	}

	// --llm needs a compile path (built-in client or hook); refuse before
	// doing any work.
	var spec compilerSpec
	if llmMode {
		spec = mustCompilerFromArgs(args)
		requireCompiler(spec, "lint --llm", "Structural `kb lint` (without --llm) needs no compiler.")
	}

	var issues []LintIssue

	// Always run structural lint
	issues = append(issues, lintStructural(scope)...)

	// Optionally run the LLM review. A failed review is
	// loud: the structural issues are still printed, then kb exits 1.
	if llmMode {
		llmIssues, err := lintLLM(scope, spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: LLM lint failed: %v\n", err)
			printLintIssues(issues, jsonOut)
			os.Exit(1)
		}
		issues = append(issues, llmIssues...)
	}

	printLintIssues(issues, jsonOut)
}

func printLintIssues(issues []LintIssue, jsonOut bool) {
	if jsonOut {
		printJSON(issues)
		return
	}

	if len(issues) == 0 {
		fmt.Println("No issues found. Knowledge base is healthy!")
		return
	}

	fmt.Printf("Found %d issues:\n\n", len(issues))
	for _, issue := range issues {
		icon := map[string]string{"error": "✗", "warning": "!", "info": "·"}[issue.Severity]
		if icon == "" {
			icon = "?"
		}
		fmt.Printf("  [%s] [%s] %s\n", icon, issue.Type, issue.Message)
		if issue.ArticleID != "" {
			fmt.Printf("      Article: %s\n", issue.ArticleID)
		}
		if issue.Suggestion != "" {
			fmt.Printf("      Fix: %s\n", issue.Suggestion)
		}
		fmt.Println()
	}
}

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

// runCategoryNormalize scans every article in the scope, clusters category
// labels that differ only by casing/whitespace/punctuation, and either reports
// the clusters (default) or rewrites articles to use the canonical form in
// each cluster (--apply).
//
// Canonical selection within a cluster: highest article count, ties broken by
// shortest original string, then alphabetical order. This favors the variant
// humans naturally picked most often while keeping choice deterministic.
func runCategoryNormalize(scope string, apply, jsonOut bool) {
	articles, err := listArticles(scope)
	if err != nil {
		fatal("Failed to list articles: %v", err)
	}
	if len(articles) == 0 {
		if jsonOut {
			printJSON(map[string]any{"scope": scope, "clusters": []any{}})
			return
		}
		fmt.Printf("No articles in scope %q.\n", scope)
		return
	}

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
	var noisy []*categoryCluster
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

	if jsonOut {
		printJSON(map[string]any{
			"scope":    scope,
			"clusters": noisy,
			"applied":  apply,
		})
		if apply {
			applyCategoryCanonical(scope, articles, noisy)
		}
		return
	}

	if len(noisy) == 0 {
		fmt.Printf("No category variants found in %q — %d normalized group(s) across %d article(s) are already consistent.\n",
			scope, len(clusters), len(articles))
		return
	}

	fmt.Printf("Category normalization — scope: %s\n\n", scope)
	fmt.Printf("Found %d cluster(s) with multiple variants:\n\n", len(noisy))
	for _, c := range noisy {
		fmt.Printf("  %q — %d articles (canonical: %q)\n", c.Key, c.Total, c.Canonical)
		// Sort variants by count desc, then alpha
		keys := make([]string, 0, len(c.Variants))
		for k := range c.Variants {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			if c.Variants[keys[i]] != c.Variants[keys[j]] {
				return c.Variants[keys[i]] > c.Variants[keys[j]]
			}
			return keys[i] < keys[j]
		})
		for _, k := range keys {
			marker := "   "
			if k == c.Canonical {
				marker = " → "
			}
			fmt.Printf("    %s%-40s (%d)\n", marker, fmt.Sprintf("%q", k), c.Variants[k])
		}
		fmt.Println()
	}

	if apply {
		changed := applyCategoryCanonical(scope, articles, noisy)
		fmt.Printf("Applied: rewrote %d article(s) to use canonical forms.\n", changed)
	} else {
		fmt.Printf("Dry run. Re-run with --apply to rewrite %d article(s).\n", affectedArticleCount(articles, noisy))
	}
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
func affectedArticleCount(articles []*WikiArticle, clusters []*categoryCluster) int {
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
func applyCategoryCanonical(scope string, articles []*WikiArticle, clusters []*categoryCluster) int {
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
