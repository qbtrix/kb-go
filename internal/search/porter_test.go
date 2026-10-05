// porter_test.go — Tests for stem (Porter step 1: plurals and -ed/-ing).
//
// Three layers:
//   1. TestStem_Canonical — hand-traced word->stem pairs through steps 1a, 1b
//      and 1c, guarding against a porting bug, and proof that steps 2-5 no
//      longer run (relational, adjustable, cease keep their suffixes).
//   2. TestStem_Guards / _Idempotent — the conservative guards (<=2 letters,
//      non-alpha tokens) and stem(stem(x))==stem(x).
//   3. TestStem_WantedConflations / _NoOverConflation — the retrieval
//      contract: inflections of one word share a stem, and the derivational
//      merges full Porter made (generating/general, use/us) do not happen.

package search

import "testing"

func TestStem_Canonical(t *testing.T) {
	cases := map[string]string{
		// 1a: plurals
		"caresses": "caress",
		"ponies":   "poni",
		"ties":     "ti",
		"caress":   "caress",
		"cats":     "cat",
		"cat":      "cat",
		// 1b: -eed/-ed/-ing, with the at/bl/iz, double-consonant and cvc fix-ups
		"feed":       "feed",
		"agreed":     "agree",
		"sing":       "sing",
		"plastered":  "plaster",
		"motoring":   "motor",
		"conflated":  "conflate",
		"troubled":   "trouble",
		"sized":      "size",
		"hopping":    "hop",
		"tanned":     "tan",
		"falling":    "fall",
		"hissing":    "hiss",
		"fizzed":     "fizz",
		"failing":    "fail",
		"filing":     "file",
		"generating": "generate",
		// 1c: y -> i when the stem has a vowel
		"happy": "happi",
		"sky":   "sky",
		// steps 2-5 are gone: derivational suffixes and final e survive
		"relational": "relational",
		"adjustable": "adjustable",
		"general":    "general",
		"experiment": "experiment",
		"cease":      "cease",
		"rate":       "rate",
		"controll":   "controll",
	}
	for in, want := range cases {
		if got := stem(in); got != want {
			t.Errorf("stem(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestStem_Guards(t *testing.T) {
	// Words <= 2 letters are never stemmed, so short tokens like "is"/"as"
	// survive intact.
	for _, w := range []string{"is", "as", "be", "a", "i", "of", "to", "on", "us"} {
		if got := stem(w); got != w {
			t.Errorf("stem(%q) = %q, want unchanged (<=2 letters)", w, got)
		}
	}
	// Non-lowercase-ASCII tokens (digits, alphanumerics) pass through untouched.
	for _, w := range []string{"123", "utf8", "s3", "abc123", "v2", "files2"} {
		if got := stem(w); got != w {
			t.Errorf("stem(%q) = %q, want unchanged (non-alpha)", w, got)
		}
	}
}

func TestStem_Idempotent(t *testing.T) {
	for _, w := range []string{
		"opens", "opening", "located", "returning", "shipped", "packages",
		"caresses", "relational", "happy", "generating", "sizes", "agreed",
	} {
		once := stem(w)
		twice := stem(once)
		if once != twice {
			t.Errorf("stem not idempotent: %q -> %q -> %q", w, once, twice)
		}
	}
}

// TestStem_WantedConflations: every inflection in a family reduces to the
// same stem, so a query in one form retrieves a document written in another.
// These are the shopper-question forms ("returning a jacket", "shipped").
func TestStem_WantedConflations(t *testing.T) {
	families := [][]string{
		{"return", "returns", "returning", "returned"},
		{"ship", "ships", "shipped", "shipping"},
		{"open", "opens", "opening", "opened"},
		{"package", "packages"},
		{"size", "sizes"},
		{"locate", "located", "locates"},
		{"query", "queries"},
		{"separate", "separated", "separating", "separates"},
	}
	for _, fam := range families {
		want := stem(fam[0])
		for _, w := range fam {
			if got := stem(w); got != want {
				t.Errorf("family %v: stem(%q) = %q, want %q (same stem as %q)",
					fam, w, got, want, fam[0])
			}
		}
	}
}

// TestStem_NoOverConflation pins the merges full Porter made and step 1 must
// not: derivational suffix stripping (-al, -ment/-ence, -ate) and final-e
// removal collapsed unrelated words into one stem, inflating document
// frequency and firing the title boost on lemmas.
func TestStem_NoOverConflation(t *testing.T) {
	pairs := [][2]string{
		{"generating", "general"},
		{"generate", "general"},
		{"experiment", "experience"},
		{"one", "on"},
		{"ones", "on"},
		{"use", "us"},
		{"open", "operator"},
		{"operate", "operator"},
		{"location", "locate"}, // nouns keep -ion: step 1 does not touch derivation
	}
	for _, p := range pairs {
		if a, b := stem(p[0]), stem(p[1]); a == b {
			t.Errorf("over-conflation: stem(%q) = stem(%q) = %q", p[0], p[1], a)
		}
	}
	// separating stays with separate (an inflection) instead of collapsing to
	// Porter's "separ", which it shared with "separately", "separation", ...
	for _, w := range []string{"separately", "separation"} {
		if stem("separating") == stem(w) {
			t.Errorf("over-conflation: stem(%q) = stem(%q) = %q", "separating", w, stem(w))
		}
	}
}
