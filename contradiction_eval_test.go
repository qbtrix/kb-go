// contradiction_eval_test.go — Runnable before/after eval for cross-source
// contradiction detection (issue #19).
// Created: 2026-06-02
// Updated: 2026-06-02 — measures the eval end-to-end through the real build path
// instead of asserting a modeled metric. Every number the eval now prints is
// derived from running cmdBuild + glossaryShow against a fixture KB on disk; the
// previously hardcoded "100% -> 0%" at-risk line is gone, replaced by a measured
// count of how many terms resolve to a non-canonical definition with no warning
// under the legacy (contradiction-off) build.
//
// Proves the feature's value, not just unit correctness. Builds a real fixture KB
// where each of the 7 high-conflict glossary terms is defined twice from two
// distinct source files: once with its wrong default prior (the meaning an LLM
// reaches for absent context) and once with its canonical workspace meaning.
// Both definitions land on disk under the same Term, so a glossary lookup
// resolves to one of them with no hint that the other contradicts it — exactly
// the silent ambiguity the domain glossary (#15) exists to prevent.
//
// BEFORE (--contradiction-mode off): the build emits zero contradiction
// warnings. glossaryShow resolves each term to the first matching article (ID
// order) and returns that single definition with no signal that a conflicting
// definition coexists. The eval measures how many terms resolve to the
// non-canonical prior under that legacy behavior.
//
// AFTER (--contradiction-mode strict): the same fixture build surfaces one
// CONTRADICTION finding per conflicting term. The disagreement is no longer
// silent.
//
// Run:
//
//	go test -run TestContradictionEval -v .
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// conflictPair is one high-conflict term with its wrong prior and canonical
// definition, sourced from two distinct files. The two files derive distinct
// article ids from their basenames, so both definitions persist on disk under
// the same Term — the on-disk ambiguity glossaryValidate and the build hook flag.
//
// glossaryShow resolves a term to the first matching article in id order, so a
// detection-off lookup deterministically returns exactly one of the two
// definitions and never reveals the other. The eval measures how many of those
// silent resolutions land on the non-canonical prior (a measured count, whatever
// the id ordering produces — not asserted to be all 7).
type conflictPair struct {
	term      string
	wrongFile string // basename -> article id; sorts before rightFile
	wrongDef  string
	rightFile string
	rightDef  string
}

// the7Conflicts mirrors the workspace's 7 high-conflict glossary terms. Each
// pairs the meaning an ungrounded model defaults to against the canonical one.
var the7Conflicts = []conflictPair{
	{
		term:      "Pocket",
		wrongFile: "pocket-clothing.md", wrongDef: "A pocket is a small fabric bag sewn into a garment to hold small items.",
		rightFile: "pocket-workspace.md", rightDef: "Pocket is a PocketPaw workspace container that holds agents, data, and tools.",
	},
	{
		term:      "Soul",
		wrongFile: "soul-religious.md", wrongDef: "The Soul is the immaterial, spiritual essence of a living being.",
		rightFile: "soul-protocol.md", rightDef: "Soul is the Soul Protocol persistent agent-identity layer with memory and personality.",
	},
	{
		term:      "Fabric",
		wrongFile: "fabric-textile.md", wrongDef: "Fabric is a woven or knitted textile material made from fibres.",
		rightFile: "fabric-ontology.md", rightDef: "Fabric is the PocketPaw typed ontology layer that links entities and relations.",
	},
	{
		term:      "Instinct",
		wrongFile: "instinct-biology.md", wrongDef: "Instinct is an innate, fixed behavioural response present in animals from birth.",
		rightFile: "instinct-runtime.md", rightDef: "Instinct is the workspace policy engine that triggers automated agent responses.",
	},
	{
		term:      "Ripple",
		wrongFile: "ripple-water.md", wrongDef: "A ripple is a small wave on the surface of a body of water.",
		rightFile: "ripple-ui.md", rightDef: "Ripple is the reactive widget runtime that renders agent UIs from typed specs.",
	},
	{
		term:      "Paw",
		wrongFile: "paw-animal.md", wrongDef: "A paw is the soft-padded foot of a four-legged mammal such as a cat or dog.",
		rightFile: "paw-runtime.md", rightDef: "Paw is the agent runtime that schedules tasks and coordinates the workspace.",
	},
	{
		term:      "Connector",
		wrongFile: "connector-hardware.md", wrongDef: "A connector is a physical plug or socket that joins two electrical wires.",
		rightFile: "connector-integration.md", rightDef: "Connector is the integration adapter that wires an external service into the workspace.",
	},
}

// writeGlossaryFile drops a hand-curated glossary source under <dir>/glossary/.
// The article id derives from the filename basename (no explicit id field), so
// two files for the same Term become two distinct on-disk articles.
func writeGlossaryFile(t *testing.T, glossaryDir, file, term, def string) {
	t.Helper()
	fm := map[string]string{"title": term, "term": term}
	fmJSON, err := json.Marshal(fm)
	if err != nil {
		t.Fatalf("marshal frontmatter for %s: %v", file, err)
	}
	body := fmt.Sprintf("---\n%s\n---\n\n%s\n", string(fmJSON), def)
	if err := os.WriteFile(filepath.Join(glossaryDir, file), []byte(body), 0o644); err != nil {
		t.Fatalf("write glossary file %s: %v", file, err)
	}
}

// buildEvalJSON runs the real cmdBuild against the fixture in --json mode (which
// suppresses the CI exit-3) and returns the parsed contradictions array. Stdout
// is captured so the JSON payload doesn't bleed into test output.
func buildEvalJSON(t *testing.T, srcDir, scope, mode string) []Contradiction {
	t.Helper()
	orig := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmdBuild([]string{srcDir, "--scope", scope, "--pattern", "*.md", "--contradiction-mode", mode, "--json"})

	w.Close()
	os.Stdout = orig

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var out struct {
		Contradictions []Contradiction `json:"contradictions"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("parse build json (mode=%s): %v\nraw: %s", mode, err, buf.String())
	}
	return out.Contradictions
}

func TestContradictionEval(t *testing.T) {
	totalConflictTerms := len(the7Conflicts)

	// --- Fixture: two real glossary source files per term, sharing an id. -----
	srcDir := t.TempDir()
	glossaryDir := filepath.Join(srcDir, "glossary")
	if err := os.MkdirAll(glossaryDir, 0o755); err != nil {
		t.Fatalf("mkdir glossary: %v", err)
	}
	for _, p := range the7Conflicts {
		// Two distinct files, same Term, distinct ids. Both persist on disk;
		// a glossary lookup silently resolves to one and never reveals the
		// other — the ambiguity this feature surfaces as a warning.
		writeGlossaryFile(t, glossaryDir, p.wrongFile, p.term, p.wrongDef)
		writeGlossaryFile(t, glossaryDir, p.rightFile, p.term, p.rightDef)
	}

	// --- BEFORE: build with detection OFF. -----------------------------------
	beforeScope := "test-contra-eval-before-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(beforeScope)) }()

	beforeFindings := buildEvalJSON(t, srcDir, beforeScope, "off")
	beforeSurfaced := len(beforeFindings) // measured: warnings emitted with detection off

	// Both source files per term persist on disk (distinct ids). Count the
	// glossary articles, then resolve each term the way a reader would
	// (glossaryShow returns the first matching article in id order) to see which
	// of the two conflicting definitions the lookup silently settles on.
	beforeArticles, err := listArticles(beforeScope)
	if err != nil {
		t.Fatalf("listArticles(before) err = %v", err)
	}
	glossaryOnDisk := 0
	for _, a := range beforeArticles {
		if a.Kind == "glossary" {
			glossaryOnDisk++
		}
	}

	// For each term, resolve it and verify the lookup returns exactly one of the
	// two conflicting definitions with no warning, then record how many of those
	// silent resolutions land on the non-canonical prior.
	silentlyResolved := 0
	nonCanonicalResolved := 0
	for _, p := range the7Conflicts {
		var sb bytes.Buffer
		if err := glossaryShow(beforeScope, p.term, &sb); err != nil {
			t.Fatalf("glossaryShow(%q) err = %v", p.term, err)
		}
		got := sb.String()
		if got != p.wrongDef && got != p.rightDef {
			t.Fatalf("glossaryShow(%q) returned an unexpected definition: %q", p.term, got)
		}
		silentlyResolved++
		if got == p.wrongDef {
			nonCanonicalResolved++
		}
	}

	// --- AFTER: rebuild the same fixture with detection ON (strict). ----------
	afterScope := "test-contra-eval-after-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(afterScope)) }()

	afterFindings := buildEvalJSON(t, srcDir, afterScope, "strict")
	afterSurfaced := len(afterFindings)
	afterSilentlyMerged := totalConflictTerms - afterSurfaced

	// --- Assertions: the measured before/after must hold. ---------------------
	if beforeSurfaced != 0 {
		t.Fatalf("detection-off build should emit 0 warnings, got %d", beforeSurfaced)
	}
	if glossaryOnDisk != totalConflictTerms*2 {
		t.Fatalf("expected %d glossary articles on disk (2 per term), got %d", totalConflictTerms*2, glossaryOnDisk)
	}
	// Every term resolved to a single definition with no warning while a
	// contradicting definition silently coexisted on disk.
	if silentlyResolved != totalConflictTerms {
		t.Fatalf("expected all %d terms to resolve silently with detection off, got %d", totalConflictTerms, silentlyResolved)
	}
	if afterSurfaced != totalConflictTerms {
		t.Fatalf("detector missed conflicts: surfaced %d of %d planted", afterSurfaced, totalConflictTerms)
	}
	if afterSilentlyMerged != 0 {
		t.Fatalf("expected 0 silent merges after detection, got %d", afterSilentlyMerged)
	}

	// --- Report the measured numbers. ----------------------------------------
	fmt.Println()
	fmt.Println("================ issue #19 contradiction eval ================")
	fmt.Printf("High-conflict glossary terms under test: %d\n", totalConflictTerms)
	fmt.Printf("Source files (2 per term, distinct ids): %d\n", totalConflictTerms*2)
	fmt.Println("--- measured end-to-end through cmdBuild + glossaryShow ------")
	fmt.Printf("BEFORE (--contradiction-mode off):\n")
	fmt.Printf("  contradiction warnings emitted:          %d\n", beforeSurfaced)
	fmt.Printf("  glossary articles on disk:               %d (2 per term)\n", glossaryOnDisk)
	fmt.Printf("  terms a lookup resolves to a single def\n")
	fmt.Printf("  with a contradicting def silent on disk: %d of %d\n", silentlyResolved, totalConflictTerms)
	fmt.Printf("  of those, lookups landing on the\n")
	fmt.Printf("  NON-canonical prior:                     %d of %d\n", nonCanonicalResolved, totalConflictTerms)
	fmt.Printf("AFTER (--contradiction-mode strict):\n")
	fmt.Printf("  contradictions surfaced for review:      %d\n", afterSurfaced)
	fmt.Printf("  terms left silently ambiguous:           %d\n", afterSilentlyMerged)
	fmt.Println("--------------------------------------------------------------")
	fmt.Println("Interpretation: every one of the 7 terms had a conflicting pair")
	fmt.Println("that previously resolved with no signal (a lookup returned one")
	fmt.Println("definition and never revealed the other contradicted it); all 7")
	fmt.Println("are now flagged for a human to resolve before that can happen.")
	fmt.Println("--------------------------------------------------------------")
	for _, c := range afterFindings {
		fmt.Printf("CONTRADICTION  %-10s  %v\n", c.Term, c.Sources)
	}
	fmt.Println("==============================================================")
	fmt.Println()
}
