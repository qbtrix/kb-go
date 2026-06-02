// contradiction_test.go — Failing-first test suite for cross-source definition
// contradiction detection (issue #19).
// Created: 2026-06-02
//
// Locks the contract for the contradiction subsystem:
//   - detectContradictions: pure, offline grouping of glossary candidates by
//     normalized term/alias key, flagging materially-different definitions
//   - the "strict" vs "loose" materially-different threshold
//   - glossaryValidate surfacing CONTRADICTION findings (on-disk path)
//   - the build/recompile hook capturing silently-overwritten same-ID candidates
//
// All tests EXPECT TO FAIL until contradiction.go and the cmdBuild/glossaryValidate
// hooks land.
package main

import (
	"os"
	"strings"
	"testing"
)

// --- 1. Pure detector: same term, two sources, different definitions ---------

func TestDetectContradictionsStrictFlagsDivergentDefinitions(t *testing.T) {
	cands := []ContradictionCandidate{
		{SourceID: "soul-religious", Term: "Soul", Definition: "The Soul is the immaterial spiritual essence of a person."},
		{SourceID: "soul-protocol", Term: "Soul", Definition: "Soul is the Soul Protocol persistent agent-identity layer."},
	}
	found := detectContradictions(cands, ContradictionConfig{Mode: "strict"})
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
	cands := []ContradictionCandidate{
		{SourceID: "a", Term: "Pocket", Definition: "Pocket is a PocketPaw workspace container."},
		{SourceID: "b", Term: "Pocket", Definition: "Pocket is a PocketPaw workspace container."},
	}
	found := detectContradictions(cands, ContradictionConfig{Mode: "strict"})
	if len(found) != 0 {
		t.Fatalf("identical definitions should not be a contradiction, got %+v", found)
	}
}

// --- 3. A single source per term is never a contradiction --------------------

func TestDetectContradictionsSingleSourceNoConflict(t *testing.T) {
	cands := []ContradictionCandidate{
		{SourceID: "only", Term: "Ripple", Definition: "Ripple is the reactive widget runtime."},
	}
	found := detectContradictions(cands, ContradictionConfig{Mode: "strict"})
	if len(found) != 0 {
		t.Fatalf("single source should not contradict itself, got %+v", found)
	}
}

// --- 4. Alias-level conflict: term on A matches an alias on B -----------------

func TestDetectContradictionsMatchesViaAlias(t *testing.T) {
	cands := []ContradictionCandidate{
		{SourceID: "fabric-textile", Term: "Fabric", Definition: "Fabric is a woven textile material."},
		{SourceID: "fabric-onto", Term: "Ontology", Aliases: []string{"Fabric"}, Definition: "Fabric is the PocketPaw typed ontology layer."},
	}
	found := detectContradictions(cands, ContradictionConfig{Mode: "strict"})
	if len(found) != 1 {
		t.Fatalf("expected 1 contradiction via alias match, got %d: %+v", len(found), found)
	}
}

// --- 5. Strict threshold: same first sentence, different tail = NOT flagged ---

func TestDetectContradictionsStrictKeyedOnFirstSentence(t *testing.T) {
	cands := []ContradictionCandidate{
		{SourceID: "a", Term: "Paw", Definition: "Paw is the agent runtime. It schedules tasks."},
		{SourceID: "b", Term: "Paw", Definition: "Paw is the agent runtime. It also handles memory."},
	}
	found := detectContradictions(cands, ContradictionConfig{Mode: "strict"})
	if len(found) != 0 {
		t.Fatalf("matching first sentence should not trip strict mode, got %+v", found)
	}
}

// --- 6. On-disk: glossaryValidate surfaces CONTRADICTION findings ------------

func TestGlossaryValidateSurfacesContradiction(t *testing.T) {
	scope := "test-contra-validate-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "soul-religious", Title: "Soul (religious)", Content: "The Soul is the immaterial spiritual essence of a being.",
		Kind: "glossary", Term: "Soul", Version: 1,
	})
	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "soul-protocol", Title: "Soul (protocol)", Content: "Soul is the Soul Protocol persistent agent-identity layer.",
		Kind: "glossary", Term: "Soul", Version: 1,
	})

	issues, err := glossaryValidate(scope)
	if err != nil {
		t.Fatalf("glossaryValidate err = %v", err)
	}
	if !containsIssue(issues, "contradiction") {
		t.Errorf("expected a CONTRADICTION finding. Got: %v", issues)
	}
	if !containsIssue(issues, "soul-religious") || !containsIssue(issues, "soul-protocol") {
		t.Errorf("contradiction should name both conflicting source ids. Got: %v", issues)
	}
}

// --- 7. On-disk: agreeing definitions produce no contradiction ---------------

func TestGlossaryValidateNoContradictionWhenAgreeing(t *testing.T) {
	scope := "test-contra-agree-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	seedGlossaryArticle(t, scope, &WikiArticle{
		ID: "pocket", Title: "Pocket", Content: "Pocket is a PocketPaw workspace container.",
		Kind: "glossary", Term: "Pocket", Version: 1,
	})

	issues, _ := glossaryValidate(scope)
	if containsIssue(issues, "contradiction") {
		t.Errorf("single clean term should not be flagged. Got: %v", issues)
	}
}

// --- 8. Snippet content is preserved for human review ------------------------

func TestContradictionSnippetsCarryBothDefinitions(t *testing.T) {
	cands := []ContradictionCandidate{
		{SourceID: "pocket-clothing", Term: "Pocket", Definition: "A pocket is a small bag sewn into clothing."},
		{SourceID: "pocket-workspace", Term: "Pocket", Definition: "Pocket is a PocketPaw workspace container."},
	}
	found := detectContradictions(cands, ContradictionConfig{Mode: "strict"})
	if len(found) != 1 {
		t.Fatalf("expected 1 contradiction, got %d", len(found))
	}
	joined := strings.Join(found[0].Snippets, " || ")
	if !strings.Contains(joined, "clothing") || !strings.Contains(joined, "workspace container") {
		t.Errorf("snippets should carry both definitions, got: %q", joined)
	}
}
