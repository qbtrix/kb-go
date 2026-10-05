// Tests for contradiction detection (issue #19): Detect groups glossary
// candidates by normalized term/alias key and flags materially different
// definitions, the "strict" (first sentence) threshold, single-source and
// identical definitions never flagged, alias matches, and findings carrying
// both definitions' snippets. glossary.Validate's CONTRADICTION findings are
// tested in internal/glossary.

package contradiction

import (
	"os"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

// --- 1. Pure detector: same term, two sources, different definitions ---------

func TestDetectContradictionsStrictFlagsDivergentDefinitions(t *testing.T) {
	cands := []Candidate{
		{SourceID: "soul-religious", Term: "Soul", Definition: "The Soul is the immaterial spiritual essence of a person."},
		{SourceID: "soul-protocol", Term: "Soul", Definition: "Soul is the Soul Protocol persistent agent-identity layer."},
	}
	found := Detect(cands, Config{Mode: "strict"})
	if len(found) != 1 {
		t.Fatalf("expected 1 contradiction, got %d: %+v", len(found), found)
	}
	c := found[0]
	if !strings.EqualFold(c.Term, "Soul") {
		t.Errorf("Term = %q, want Soul", c.Term)
	}
	if len(c.Sources) != 2 {
		t.Errorf("Sources = %v, want 2 source ids", c.Sources)
	}
	if len(c.Snippets) != 2 {
		t.Errorf("Snippets = %v, want 2 definition snippets", c.Snippets)
	}
}

// --- 2. Identical definitions across sources are NOT a contradiction ---------

func TestDetectContradictionsIgnoresIdenticalDefinitions(t *testing.T) {
	cands := []Candidate{
		{SourceID: "a", Term: "Pocket", Definition: "Pocket is a PocketPaw workspace container."},
		{SourceID: "b", Term: "Pocket", Definition: "Pocket is a PocketPaw workspace container."},
	}
	found := Detect(cands, Config{Mode: "strict"})
	if len(found) != 0 {
		t.Fatalf("identical definitions should not be a contradiction, got %+v", found)
	}
}

// --- 3. A single source per term is never a contradiction --------------------

func TestDetectContradictionsSingleSourceNoConflict(t *testing.T) {
	cands := []Candidate{
		{SourceID: "only", Term: "Ripple", Definition: "Ripple is the reactive widget runtime."},
	}
	found := Detect(cands, Config{Mode: "strict"})
	if len(found) != 0 {
		t.Fatalf("single source should not contradict itself, got %+v", found)
	}
}

// --- 4. Alias-level conflict: term on A matches an alias on B -----------------

func TestDetectContradictionsMatchesViaAlias(t *testing.T) {
	cands := []Candidate{
		{SourceID: "fabric-textile", Term: "Fabric", Definition: "Fabric is a woven textile material."},
		{SourceID: "fabric-onto", Term: "Ontology", Aliases: []string{"Fabric"}, Definition: "Fabric is the PocketPaw typed ontology layer."},
	}
	found := Detect(cands, Config{Mode: "strict"})
	if len(found) != 1 {
		t.Fatalf("expected 1 contradiction via alias match, got %d: %+v", len(found), found)
	}
}

// --- 5. Strict threshold: same first sentence, different tail = NOT flagged ---

func TestDetectContradictionsStrictKeyedOnFirstSentence(t *testing.T) {
	cands := []Candidate{
		{SourceID: "a", Term: "Paw", Definition: "Paw is the agent runtime. It schedules tasks."},
		{SourceID: "b", Term: "Paw", Definition: "Paw is the agent runtime. It also handles memory."},
	}
	found := Detect(cands, Config{Mode: "strict"})
	if len(found) != 0 {
		t.Fatalf("matching first sentence should not trip strict mode, got %+v", found)
	}
}

// --- 6. On-disk: glossary.Validate surfaces CONTRADICTION findings ------------

// --- 7. On-disk: agreeing definitions produce no contradiction ---------------

// --- 8. Snippet content is preserved for human review ------------------------

func TestContradictionSnippetsCarryBothDefinitions(t *testing.T) {
	cands := []Candidate{
		{SourceID: "pocket-clothing", Term: "Pocket", Definition: "A pocket is a small bag sewn into clothing."},
		{SourceID: "pocket-workspace", Term: "Pocket", Definition: "Pocket is a PocketPaw workspace container."},
	}
	found := Detect(cands, Config{Mode: "strict"})
	if len(found) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(found))
	}
	joined := strings.Join(found[0].Snippets, " || ")
	if !strings.Contains(joined, "clothing") || !strings.Contains(joined, "workspace container") {
		t.Errorf("snippets should carry both definitions, got: %q", joined)
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
