// context_excerpt_test.go covers contextExcerpt's edge cases directly (budget
// smaller than the title, articles without headings, a table that alone is
// over budget, queries that match nothing) and the --context --json mode.
package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

func sizeGuideArticle(t *testing.T) *WikiArticle {
	t.Helper()
	body, err := os.ReadFile(kbtest.Path(t, "testdata", "size-guide-article.md"))
	if err != nil {
		t.Fatal(err)
	}
	return &WikiArticle{ID: "size-guide", Title: "Cairn & Co. Size Guide",
		Summary: "Sizing charts for jackets, packs, footwear and socks.",
		Content: strings.ReplaceAll(string(body), "\r\n", "\n")}
}

// assertWholeTableRows fails if any emitted table line is cut mid-row.
func assertWholeTableRows(t *testing.T, out string) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		l := strings.TrimSpace(line)
		if strings.HasPrefix(l, "|") && !strings.HasSuffix(l, "|") {
			t.Fatalf("table row cut mid-way: %q\n%s", l, out)
		}
	}
}

func TestContextExcerptWholeArticleFits(t *testing.T) {
	a := &WikiArticle{Title: "Returns", Summary: "60-day returns.", Content: "Return unworn gear within 60 days."}
	got := contextExcerpt(a, "returns", 4000)
	if got != "## Returns\nReturn unworn gear within 60 days." {
		t.Fatalf("got %q", got)
	}
}

func TestContextExcerptBudgetSmallerThanTitle(t *testing.T) {
	a := sizeGuideArticle(t)
	for _, budget := range []int{0, 1, 5, 12, 26} {
		got := contextExcerpt(a, "footwear", budget)
		if len(got) > budget {
			t.Errorf("budget %d: got %d bytes %q", budget, len(got), got)
		}
		if strings.Contains(got, "\n") {
			t.Errorf("budget %d: expected only a (cut) title line, got %q", budget, got)
		}
	}
	// Multi-byte title: the cut must stay on a rune boundary.
	a.Title = "Größentabelle für Schuhe und Jacken"
	got := contextExcerpt(a, "x", 10)
	if !strings.HasPrefix(got, "## ") || len(got) > 10 || !utf8.ValidString(got) {
		t.Errorf("bad multi-byte cut %q", got)
	}
}

func TestContextExcerptNoHeadings(t *testing.T) {
	var paras []string
	for i := 0; i < 30; i++ {
		p := "Plain paragraph about shipping times and carriers."
		if i == 20 {
			p = "Oversized freight like kayaks ships by pallet within ten days."
		}
		paras = append(paras, p)
	}
	a := &WikiArticle{Title: "Shipping", Content: strings.Join(paras, "\n\n")}
	got := contextExcerpt(a, "kayak pallet", 300)
	if len(got) > 300 || !strings.HasPrefix(got, "## Shipping\n") {
		t.Fatalf("len %d: %q", len(got), got)
	}
	if !strings.Contains(got, "pallet") {
		t.Errorf("expected the matching paragraph, got %q", got)
	}
}

func TestContextExcerptTableAloneOverBudget(t *testing.T) {
	rows := []string{"| Size | EU | Length (cm) |", "|---|---|---|"}
	for i := 0; i < 80; i++ {
		rows = append(rows, "| "+strings.Repeat("9", i%5+1)+" | 44 | 28.0 |")
	}
	a := &WikiArticle{Title: "Chart", Content: "## Footwear\n\n" + strings.Join(rows, "\n")}
	got := contextExcerpt(a, "footwear", 500)
	if len(got) > 500 {
		t.Fatalf("over budget: %d", len(got))
	}
	assertWholeTableRows(t, got)
	if !strings.Contains(got, rows[0]+"\n"+rows[1]+"\n"+rows[2]) {
		t.Errorf("expected header, separator and first rows, got:\n%s", got)
	}
	if !strings.Contains(got, "## Footwear") {
		t.Errorf("expected the section heading, got:\n%s", got)
	}
}

func TestContextExcerptQueryMatchesNothingUsesHead(t *testing.T) {
	a := sizeGuideArticle(t)
	got := contextExcerpt(a, "zzqx unmatched", 700)
	if len(got) > 700 {
		t.Fatalf("over budget: %d", len(got))
	}
	if !strings.Contains(got, "measure over a base layer") {
		t.Errorf("expected the article head, got:\n%s", got)
	}
	if got == "## Cairn & Co. Size Guide\n"+a.Summary {
		t.Error("fell back to summary-only")
	}
	assertWholeTableRows(t, got)
}

func TestContextExcerptHeadingOnlyMatchAndOrder(t *testing.T) {
	a := sizeGuideArticle(t)
	// "camp mocs" appears only in a heading; "torso" only in the backpacks section.
	got := contextExcerpt(a, "torso camp mocs", 1500)
	if len(got) > 1500 {
		t.Fatalf("over budget: %d", len(got))
	}
	pi, si := strings.Index(got, "## Backpacks"), strings.Index(got, "## Socks & Camp Mocs")
	if pi < 0 || si < 0 || pi > si {
		t.Fatalf("expected both sections in document order, got:\n%s", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("expected a skip marker between non-adjacent sections, got:\n%s", got)
	}
}

func TestContextBudgetsEveryBlockAndKeepsFirstHit(t *testing.T) {
	a := sizeGuideArticle(t)
	b := &WikiArticle{ID: "b", Title: "Socks", Content: strings.Repeat("Merino socks for footwear. ", 100)}
	// Total smaller than the first article: it must be trimmed, not dropped.
	blocks := searchContextBlocks([]*WikiArticle{a, b}, "footwear", 4000, 1000)
	if len(blocks) == 0 || blocks[0].Article != a || len(blocks[0].Block) > 1000 || !blocks[0].Truncated {
		t.Fatalf("first hit dropped or over total: %+v", blocks)
	}
	out := formatSearchContext([]*WikiArticle{a, b}, "footwear", 1500, 8000)
	for _, blk := range strings.Split(out, contextSeparator) {
		if len(blk) > 1500 {
			t.Errorf("block over budget: %d", len(blk))
		}
	}
	if len(out) > 8000 {
		t.Errorf("total over cap: %d", len(out))
	}
}

// Content with a markdown rule ("---" between blank lines) would be split by a
// consumer that splits the text output on the separator; --json keeps it whole.
func TestContextJSONKeepsHorizontalRules(t *testing.T) {
	scope := "test-ctx-json-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()
	content := "Intro about the warranty.\n\n---\n\nTail: lifetime repair guarantee on all packs."
	if err := saveArticle(scope, &WikiArticle{ID: "warranty", Title: "Warranty", Summary: "Repairs.",
		Content: content, SourcePath: "warranty", Version: 1}); err != nil {
		t.Fatal(err)
	}
	out := runSearchContext(t, "warranty repair", "--scope", scope, "--limit", "3", "--json")
	var got []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].ID != "warranty" || got[0].Title != "Warranty" || got[0].Truncated {
		t.Fatalf("unexpected entries: %+v", got)
	}
	if got[0].Text != content {
		t.Errorf("text not intact:\n%q\nwant\n%q", got[0].Text, content)
	}
}
