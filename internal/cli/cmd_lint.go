// Implements `kb lint`: structural lint, the --llm review through the compiler
// hook, and the --normalize-categories mode (clustering in internal/lint);
// prints issues as text or JSON.

package cli

import (
	"fmt"
	"os"
	"sort"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/lint"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
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
	var spec compile.Spec
	if llmMode {
		spec = mustCompilerFromArgs(args)
		requireCompiler(spec, "lint --llm", "Structural `kb lint` (without --llm) needs no compiler.")
	}

	var issues []model.LintIssue

	// Always run structural lint
	issues = append(issues, lint.Structural(scope)...)

	// Optionally run the LLM review. A failed review is
	// loud: the structural issues are still printed, then kb exits 1.
	if llmMode {
		llmIssues, err := lint.LLM(scope, spec)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: LLM lint failed: %v\n", err)
			printLintIssues(issues, jsonOut)
			os.Exit(1)
		}
		issues = append(issues, llmIssues...)
	}

	printLintIssues(issues, jsonOut)
}

func printLintIssues(issues []model.LintIssue, jsonOut bool) {
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

// runCategoryNormalize scans every article in the scope, clusters category
// labels that differ only by casing/whitespace/punctuation, and either reports
// the clusters (default) or rewrites articles to use the canonical form in
// each cluster (--apply).
//
// Canonical selection within a cluster: highest article count, ties broken by
// shortest original string, then alphabetical order. This favors the variant
// humans naturally picked most often while keeping choice deterministic.
func runCategoryNormalize(scope string, apply, jsonOut bool) {
	articles, err := store.ListArticles(scope)
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

	noisy, groups := lint.ClusterCategories(articles)

	if jsonOut {
		printJSON(map[string]any{
			"scope":    scope,
			"clusters": noisy,
			"applied":  apply,
		})
		if apply {
			lint.ApplyCanonical(scope, articles, noisy)
		}
		return
	}

	if len(noisy) == 0 {
		fmt.Printf("No category variants found in %q — %d normalized group(s) across %d article(s) are already consistent.\n",
			scope, groups, len(articles))
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
		changed := lint.ApplyCanonical(scope, articles, noisy)
		fmt.Printf("Applied: rewrote %d article(s) to use canonical forms.\n", changed)
	} else {
		fmt.Printf("Dry run. Re-run with --apply to rewrite %d article(s).\n", lint.AffectedCount(articles, noisy))
	}
}
