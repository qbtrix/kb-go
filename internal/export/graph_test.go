// Tests for the concept graph: shared-article counting, ConceptGraph, the
// mermaid renderer and its label escaping.

package export

import (
	"os"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
)

func TestCountSharedArticles(t *testing.T) {
	a := []string{"x", "y", "z"}
	b := []string{"y", "z", "w"}
	if got := countSharedArticles(a, b); got != 2 {
		t.Errorf("countSharedArticles = %d, want 2", got)
	}
	if got := countSharedArticles(a, []string{}); got != 0 {
		t.Errorf("countSharedArticles empty = %d, want 0", got)
	}
	if got := countSharedArticles(a, []string{"q", "r"}); got != 0 {
		t.Errorf("countSharedArticles disjoint = %d, want 0", got)
	}
}

func TestBuildConceptGraph(t *testing.T) {
	idx := &model.KnowledgeIndex{
		Concepts: map[string]*model.Concept{
			"auth":    {Name: "auth", Articles: []string{"a1", "a2", "a3"}},
			"jwt":     {Name: "jwt", Articles: []string{"a1", "a2"}},
			"session": {Name: "session", Articles: []string{"a2", "a3"}},
			"orphan":  {Name: "orphan", Articles: []string{"a5"}}, // below minArticles
		},
	}
	nodes, edges := ConceptGraph(idx, 10, 2)
	if len(nodes) != 3 {
		t.Errorf("nodes = %d, want 3 (orphan should be filtered)", len(nodes))
	}
	// auth-jwt share a1,a2 ; auth-session share a2,a3 ; jwt-session share a2
	if len(edges) != 3 {
		t.Errorf("edges = %d, want 3", len(edges))
	}
}

func TestRenderMermaid(t *testing.T) {
	nodes := []Node{
		{ID: "c0", Label: "auth", Kind: "concept", Size: 3},
		{ID: "c1", Label: "jwt", Kind: "concept", Size: 2},
	}
	edges := []Edge{{Source: "c0", Target: "c1", Weight: 2}}
	out := Mermaid(nodes, edges, "")
	if !strings.Contains(out, "graph LR") {
		t.Error("missing graph LR header")
	}
	if !strings.Contains(out, "c0([\"auth\"])") {
		t.Error("missing concept node for auth")
	}
	if !strings.Contains(out, "c0 --- c1") {
		t.Error("missing edge")
	}
}

func TestEscapeMermaid(t *testing.T) {
	if got := escapeMermaid("hello \"world\""); got != "hello 'world'" {
		t.Errorf("escapeMermaid quotes = %q, want %q", got, "hello 'world'")
	}
	if got := escapeMermaid("line1\nline2"); got != "line1 line2" {
		t.Errorf("escapeMermaid newline = %q", got)
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
