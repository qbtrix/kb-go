// Knowledge-base lint: structural checks that need no LLM, and an LLM review
// for inconsistencies, gaps, missing connections and stale articles. The LLM
// review takes the same compile path as build (compile.go): the --compiler
// hook when one is configured, else the built-in Anthropic client.

package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- Structural Lint (no LLM) ---

func lintStructural(scope string) []LintIssue {
	articles, _ := listArticles(scope)
	idx := loadIndex(scope)
	var issues []LintIssue

	if len(articles) == 0 {
		issues = append(issues, LintIssue{
			Type: "gap", Severity: "warning",
			Message: "Knowledge base is empty — no articles found.",
		})
		return issues
	}

	articleIDs := map[string]bool{}
	for _, a := range articles {
		articleIDs[a.ID] = true
	}

	for _, a := range articles {
		// Check for empty content
		if strings.TrimSpace(a.Content) == "" {
			issues = append(issues, LintIssue{
				Type: "gap", Severity: "error",
				Message:   fmt.Sprintf("Article '%s' has empty content", a.Title),
				ArticleID: a.ID,
			})
		}

		// Check for missing concepts
		if len(a.Concepts) == 0 {
			issues = append(issues, LintIssue{
				Type: "gap", Severity: "warning",
				Message:    fmt.Sprintf("Article '%s' has no concepts", a.Title),
				ArticleID:  a.ID,
				Suggestion: "Recompile to extract concepts",
			})
		}

		// Check for broken backlinks
		for _, link := range a.Backlinks {
			if !articleIDs[link] {
				issues = append(issues, LintIssue{
					Type: "connection", Severity: "warning",
					Message:    fmt.Sprintf("Article '%s' has broken backlink to '%s'", a.Title, link),
					ArticleID:  a.ID,
					Suggestion: "Remove broken backlink or create missing article",
				})
			}
		}

		// Check for missing summary
		if strings.TrimSpace(a.Summary) == "" {
			issues = append(issues, LintIssue{
				Type: "gap", Severity: "info",
				Message:    fmt.Sprintf("Article '%s' has no summary", a.Title),
				ArticleID:  a.ID,
				Suggestion: "Recompile to generate summary",
			})
		}
	}

	// Check for orphan concepts (in index but no articles reference them)
	for key, c := range idx.Concepts {
		alive := false
		for _, aid := range c.Articles {
			if articleIDs[aid] {
				alive = true
				break
			}
		}
		if !alive {
			issues = append(issues, LintIssue{
				Type: "stale", Severity: "info",
				Message:    fmt.Sprintf("Concept '%s' (%s) has no live articles", c.Name, key),
				Suggestion: "Rebuild index to clean up",
			})
		}
	}

	// Check for island articles (no backlinks to or from)
	for _, a := range articles {
		if len(a.Backlinks) == 0 {
			linkedTo := false
			for _, other := range articles {
				if other.ID == a.ID {
					continue
				}
				if contains(other.Backlinks, a.ID) {
					linkedTo = true
					break
				}
			}
			if !linkedTo && len(articles) > 1 {
				issues = append(issues, LintIssue{
					Type: "connection", Severity: "info",
					Message:    fmt.Sprintf("Article '%s' is isolated (no backlinks)", a.Title),
					ArticleID:  a.ID,
					Suggestion: "Consider linking to related articles",
				})
			}
		}
	}

	return issues
}

// --- LLM Lint ---

// buildLintPrompt renders the audit prompt over every article's metadata.
func buildLintPrompt(articles []*WikiArticle) string {
	var sb strings.Builder
	for _, a := range articles {
		fmt.Fprintf(&sb, "## %s (id: %s)\nSummary: %s\nConcepts: %s\nCategories: %s\nBacklinks: %s\n\n",
			a.Title, a.ID, a.Summary,
			strings.Join(a.Concepts, ", "),
			strings.Join(a.Categories, ", "),
			strings.Join(a.Backlinks, ", "))
	}

	return fmt.Sprintf(`You are a knowledge base auditor. Review this knowledge base and find issues.

Look for:
- INCONSISTENCY: articles that contradict each other
- GAP: important topics mentioned but not covered by any article
- CONNECTION: related articles that should reference each other but don't
- STALE: articles that seem outdated or need recompilation

Output ONLY a JSON array:
[{"type":"gap","severity":"warning","message":"...","article_id":"...","suggestion":"..."}]

If no issues, output: []

Knowledge base:
%s`, sb.String())
}

// lintLLM runs the LLM review through the configured compile path and parses
// the JSON array of issues the model returns. Unparseable output is an error.
func lintLLM(scope string, spec compilerSpec) ([]LintIssue, error) {
	articles, _ := listArticles(scope)
	if len(articles) == 0 {
		return []LintIssue{{
			Type: "gap", Severity: "warning",
			Message: "Knowledge base is empty.",
		}}, nil
	}

	prompt := buildLintPrompt(articles)
	var out string
	switch {
	case spec.hook():
		b, err := runCompiler(spec, prompt, "lint")
		if err != nil {
			return nil, err
		}
		out = string(b)
	case spec.builtin():
		text, _, err := callAnthropic(spec, "You are a knowledge base auditor. Output only valid JSON arrays.", prompt)
		if err != nil {
			return nil, err
		}
		out = text
	default:
		return nil, fmt.Errorf("no compiler configured")
	}
	text := stripFences(out)
	var issues []LintIssue
	if err := json.Unmarshal([]byte(text), &issues); err != nil {
		i, j := strings.Index(text, "["), strings.LastIndex(text, "]")
		if i < 0 || j <= i || json.Unmarshal([]byte(text[i:j+1]), &issues) != nil {
			return nil, fmt.Errorf("lint output is not a JSON array of issues: %v; output starts: %q", err, text[:min(len(text), 200)])
		}
	}
	return issues, nil
}
