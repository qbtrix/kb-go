// Knowledge-base lint: structural checks that need no LLM, and an LLM review
// for inconsistencies, gaps, missing connections and stale articles. The LLM
// review goes through the caller's --compiler hook (compile.go); kb itself
// holds no LLM client.

package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/model"
)

// --- Structural Lint (no LLM) ---

func lintStructural(scope string) []model.LintIssue {
	articles, _ := listArticles(scope)
	idx := loadIndex(scope)
	var issues []model.LintIssue

	if len(articles) == 0 {
		issues = append(issues, model.LintIssue{
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
			issues = append(issues, model.LintIssue{
				Type: "gap", Severity: "error",
				Message:   fmt.Sprintf("Article '%s' has empty content", a.Title),
				ArticleID: a.ID,
			})
		}

		// Check for missing concepts
		if len(a.Concepts) == 0 {
			issues = append(issues, model.LintIssue{
				Type: "gap", Severity: "warning",
				Message:    fmt.Sprintf("Article '%s' has no concepts", a.Title),
				ArticleID:  a.ID,
				Suggestion: "Recompile to extract concepts",
			})
		}

		// Check for broken backlinks
		for _, link := range a.Backlinks {
			if !articleIDs[link] {
				issues = append(issues, model.LintIssue{
					Type: "connection", Severity: "warning",
					Message:    fmt.Sprintf("Article '%s' has broken backlink to '%s'", a.Title, link),
					ArticleID:  a.ID,
					Suggestion: "Remove broken backlink or create missing article",
				})
			}
		}

		// Check for missing summary
		if strings.TrimSpace(a.Summary) == "" {
			issues = append(issues, model.LintIssue{
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
			issues = append(issues, model.LintIssue{
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
				if slices.Contains(other.Backlinks, a.ID) {
					linkedTo = true
					break
				}
			}
			if !linkedTo && len(articles) > 1 {
				issues = append(issues, model.LintIssue{
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
func buildLintPrompt(articles []*model.WikiArticle) string {
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

// lintWithHook runs the LLM review through the compiler hook and parses the
// JSON array of issues it prints. Unparseable output is an error.
func lintWithHook(scope string, spec compile.Spec) ([]model.LintIssue, error) {
	articles, _ := listArticles(scope)
	if len(articles) == 0 {
		return []model.LintIssue{{
			Type: "gap", Severity: "warning",
			Message: "Knowledge base is empty.",
		}}, nil
	}

	out, err := compile.Run(spec, buildLintPrompt(articles), "lint")
	if err != nil {
		return nil, err
	}
	text := compile.StripFences(string(out))
	var issues []model.LintIssue
	if err := json.Unmarshal([]byte(text), &issues); err != nil {
		i, j := strings.Index(text, "["), strings.LastIndex(text, "]")
		if i < 0 || j <= i || json.Unmarshal([]byte(text[i:j+1]), &issues) != nil {
			return nil, fmt.Errorf("lint output is not a JSON array of issues: %v; output starts: %q", err, text[:min(len(text), 200)])
		}
	}
	return issues, nil
}
