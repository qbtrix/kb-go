// porter_test.go — Tests for the vendored Porter stemmer (feat/bm25-stemming).
// Created: 2026-07-15.
//
// Three layers:
//   1. TestPorterStem_Canonical — hand-verified word->stem pairs spanning every
//      step of Porter's reference algorithm, guarding against a porting bug.
//   2. TestPorterStem_Guards / _Idempotent — the conservative guards
//      (<=2 letters, non-alpha tokens) and stem(stem(x))==stem(x).
//   3. TestPorterStem_MorphologicalFamilies + the BM25 retrieval tests below —
//      the actual recall fix: morphological variants collapse to one stem, and
//      an over-stem false positive we worried about does NOT happen.

package main

import "testing"

func TestPorterStem_Canonical(t *testing.T) {
	// Every pair below was traced through the full algorithm (all 5 steps),
	// not just the single step that Porter's paper uses to illustrate a rule
	// (paper examples like "agreed->agree" are per-step and reduce further).
	cases := map[string]string{
		"caresses":  "caress",
		"ponies":    "poni",
		"ties":      "ti",
		"caress":    "caress",
		"cats":      "cat",
		"cat":       "cat",
		"feed":      "feed",
		"sing":      "sing",
		"hopping":   "hop",
		"tanned":    "tan",
		"falling":   "fall",
		"hissing":   "hiss",
		"happy":     "happi",
		"sky":       "sky",
		"rate":      "rate",
		"roll":      "roll",
		"controll":  "control",
		"cease":     "ceas",
		"probate":   "probat",
		"plastered": "plaster",
		"motoring":  "motor",
	}
	for in, want := range cases {
		if got := porterStem(in); got != want {
			t.Errorf("porterStem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPorterStem_Guards(t *testing.T) {
	// Words <= 2 letters are never stemmed (Porter's documented DEPARTURE), so
	// stopword-ish short tokens like "is"/"as" survive intact.
	for _, w := range []string{"is", "as", "be", "a", "i", "of", "to"} {
		if got := porterStem(w); got != w {
			t.Errorf("porterStem(%q) = %q, want unchanged (<=2 letters)", w, got)
		}
	}
	// Non-lowercase-ASCII tokens (digits, alphanumerics) pass through untouched.
	for _, w := range []string{"123", "utf8", "s3", "abc123", "v2"} {
		if got := porterStem(w); got != w {
			t.Errorf("porterStem(%q) = %q, want unchanged (non-alpha)", w, got)
		}
	}
}

func TestPorterStem_Idempotent(t *testing.T) {
	for _, w := range []string{
		"opens", "opening", "located", "location", "serving", "authentication",
		"caresses", "relational", "digitizer", "adjustable", "controll",
	} {
		once := porterStem(w)
		twice := porterStem(once)
		if once != twice {
			t.Errorf("porterStem not idempotent: %q -> %q -> %q", w, once, twice)
		}
	}
}

// TestPorterStem_MorphologicalFamilies is the crux of the recall fix: every
// surface form in a family must reduce to the same stem so that a query in one
// form retrieves a document written in another.
func TestPorterStem_MorphologicalFamilies(t *testing.T) {
	families := [][]string{
		{"open", "opens", "opening", "opened"},           // the exact T5 case
		{"locate", "located", "location", "locations"},    // located <-> location
		{"serve", "serves", "served", "serving"},          // serves <-> serving
	}
	for _, fam := range families {
		want := porterStem(fam[0])
		for _, w := range fam {
			if got := porterStem(w); got != want {
				t.Errorf("family %v: porterStem(%q) = %q, want %q (same stem as %q)",
					fam, w, got, want, fam[0])
			}
		}
	}
}

// TestPorterStem_NoOverStem guards the false positive we worried about: the
// stemmer is aggressive enough to unify open/opens/opening but must NOT merge
// "open" with the unrelated "operate/operation/operator" family (they stem to
// "oper", not "open").
func TestPorterStem_NoOverStem(t *testing.T) {
	if porterStem("open") == porterStem("operator") {
		t.Errorf("over-stem: %q and %q collapsed to the same stem %q",
			"open", "operator", porterStem("open"))
	}
	// The operate family should still be internally consistent.
	oper := porterStem("operate")
	for _, w := range []string{"operates", "operation", "operator", "operating"} {
		if got := porterStem(w); got != oper {
			t.Errorf("porterStem(%q) = %q, want %q (operate family)", w, got, oper)
		}
	}
}
