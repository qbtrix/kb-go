// context_passages_test.go pins what `kb search --context` hands an agent:
// the article body (or the query-relevant part of it), never just the summary.
// Fixture testdata/size-guide-article.md is a real compiled store size guide
// whose footwear table sits past the old 2,000-char cut-off; a shopping
// assistant asked "tell me guide for shoe sizes" got only the summary back and
// told the visitor the chart wasn't available.
package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const shoeRow = "| 10 | 11.5 | 9 | 44 | 28.0 |"

func runSearchContext(t *testing.T, args ...string) string {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	cmdSearch(append(args, "--context"))
	w.Close()
	os.Stdout = orig
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func seedSizeGuide(t *testing.T, scope string) {
	t.Helper()
	body, err := os.ReadFile("testdata/size-guide-article.md")
	if err != nil {
		t.Fatal(err)
	}
	a := &WikiArticle{
		ID:         "cairn-co-size-guide",
		Title:      "Cairn & Co. Size Guide",
		Summary:    "Sizing charts for jackets, packs, footwear and socks. Free exchanges are offered.",
		Content:    strings.ReplaceAll(string(body), "\r\n", "\n"),
		Concepts:   []string{"size guide", "footwear sizing"},
		SourcePath: "size-guide",
		Version:    1,
	}
	if err := saveArticle(scope, a); err != nil {
		t.Fatal(err)
	}
	other := &WikiArticle{ID: "returns", Title: "Returns and exchanges", Summary: "60-day returns.",
		Content: "Return unworn gear within 60 days for a full refund.", SourcePath: "returns", Version: 1}
	if err := saveArticle(scope, other); err != nil {
		t.Fatal(err)
	}
}

// The whole article fits a normal budget: it must come back whole, table included.
func TestContextReturnsBodyNotSummary(t *testing.T) {
	scope := "test-ctx-body-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()
	seedSizeGuide(t, scope)

	out := runSearchContext(t, "tell me guide for shoe sizes", "--scope", scope, "--limit", "1")
	if !strings.Contains(out, shoeRow) {
		t.Fatalf("--context dropped the footwear table; got:\n%s", out)
	}
	if strings.Count(out, "## Cairn & Co. Size Guide") != 1 {
		t.Errorf("article title should appear once, got:\n%s", out)
	}
}

// A caller with less room than the article passes --context-chars; kb-go must
// spend that budget on the sections the query is about, not the article's head.
func TestContextBudgetKeepsQueryRelevantSection(t *testing.T) {
	scope := "test-ctx-budget-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()
	seedSizeGuide(t, scope)

	out := runSearchContext(t, "footwear shoe size conversion EU", "--scope", scope, "--limit", "1", "--context-chars", "1200")
	block := strings.SplitN(out, "\n\n---\n\n", 2)[0]
	if len(block) > 1200 {
		t.Errorf("block is %d chars, over the 1200 budget", len(block))
	}
	if !strings.Contains(block, shoeRow) {
		t.Fatalf("budgeted context missed the footwear table the query asked for; got:\n%s", block)
	}

	out = runSearchContext(t, "backpack torso length measure", "--scope", scope, "--limit", "1", "--context-chars", "1200")
	block = strings.SplitN(out, "\n\n---\n\n", 2)[0]
	if !strings.Contains(strings.ToLower(block), "torso") || strings.Contains(block, shoeRow) {
		t.Fatalf("query about pack torso length should pick the backpacks section, got:\n%s", block)
	}
}

// Markdown tables must never be cut mid-row: a half table reads as data loss.
func TestContextNeverSplitsTableRows(t *testing.T) {
	scope := "test-ctx-rows-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()
	seedSizeGuide(t, scope)

	out := runSearchContext(t, "footwear EU sizes", "--scope", scope, "--limit", "1", "--context-chars", "900")
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "|") && !strings.HasSuffix(l, "|") {
			t.Fatalf("table row cut mid-way: %q\nfull output:\n%s", l, out)
		}
	}
}
