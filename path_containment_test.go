// path_containment_test.go — Traversal-containment tests for the read
// primitives (issue #23). Both primitives take untrusted, path-like input that
// the `kb serve` MCP surface now feeds with agent-controlled arguments over a
// persistent connection:
//
//   - loadArticle(scope, id): id is joined into the scope's wiki dir. A
//     traversal id like "../../../../etc/hosts" escapes the scope after
//     filepath.Join cleans it. These tests pin that loadArticle rejects ids
//     carrying path separators or "..", covering the kb show / kb_show path.
//   - loadVectorFromFile / the MCP query_vec_path branch: must refuse a path
//     outside its allowed directory and must not leak file contents back in the
//     returned error string.
//
// Written test-first per the repo's bug convention: they reproduce the escape
// and fail before the fix lands.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- loadArticle id containment ---

func TestLoadArticle_RejectsTraversalID(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "contain-" + filepath.Base(dir)
	ensureDirs(scope)

	// Prove the escape, not a coincidental not-found. id="../../secret" resolves
	// after filepath.Join's Clean to <base>/secret.md — outside the scope's wiki
	// AND outside the scope dir. Plant a readable article body there so a
	// successful (vulnerable) read returns its contents; containment must reject
	// before the read.
	escapeTarget := filepath.Join(basePath(), "secret.md")
	if err := os.WriteFile(escapeTarget, []byte("# Leaked\n\nTOP SECRET"), 0o644); err != nil {
		t.Fatalf("plant escape target: %v", err)
	}
	// Sanity: confirm the planted file is exactly where "../../secret" lands.
	resolved := filepath.Join(scopeDir(scope), "wiki", "../../secret"+".md")
	if resolved != escapeTarget {
		t.Fatalf("test assumption broke: %q != %q", resolved, escapeTarget)
	}

	bad := []string{
		"../../secret",          // the proven escape target above
		"../../../../etc/hosts", // classic deep traversal
		"../secret",
		"sub/../../secret",
		"a/b/c",
		`..\..\secret`,
		`a\b`,
		"/etc/hosts",
		"..",
	}
	for _, id := range bad {
		t.Run(id, func(t *testing.T) {
			a, err := loadArticle(scope, id)
			if err == nil {
				t.Fatalf("loadArticle(%q) returned no error; traversal not contained (got article %+v)", id, a)
			}
			if a != nil {
				t.Fatalf("loadArticle(%q) returned a non-nil article on a rejected id", id)
			}
			// A vulnerable read would surface the planted body either as a
			// returned article or echoed in the error.
			if strings.Contains(err.Error(), "TOP SECRET") {
				t.Fatalf("loadArticle(%q) leaked file contents in error: %v", id, err)
			}
		})
	}
}

func TestLoadArticle_AllowsLegitimateSlugIDs(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "contain-ok-" + filepath.Base(dir)

	// All ids kb-go actually generates: slugify() output (lowercase, digits,
	// hyphens), contentHash hex, and term slugs. None contain a separator or
	// "..". Each must still resolve after containment.
	ids := []string{
		"my-article",
		"rate-limiter-pattern",
		"a1b2c3d4e5f6a7b8", // contentHash[:16] shape
		"pocket",           // glossary term slug
		"soul-protocol",
		"single",
	}
	for _, id := range ids {
		stubArticle(t, scope, id, "Title "+id, "summary", "# Body\n\ncontent")
	}
	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			a, err := loadArticle(scope, id)
			if err != nil {
				t.Fatalf("loadArticle(%q) rejected a legitimate id: %v", id, err)
			}
			if a == nil || a.ID != id {
				t.Fatalf("loadArticle(%q) did not round-trip; got %+v", id, a)
			}
		})
	}
}

// --- loadVectorFromFile / MCP query_vec_path containment ---

func TestMCPSearch_QueryVecPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "vec-contain-" + filepath.Base(dir)
	ensureDirs(scope)

	// Plant a readable JSON file OUTSIDE the kb base so a traversal path could
	// load it. Its contents are a valid vector so a successful read would parse
	// and the only thing stopping it is containment.
	outside := filepath.Join(dir, "outside-vector.json")
	if err := os.WriteFile(outside, []byte(`{"vector":[0.1,0.2],"marker":"LEAKED-CONTENTS"}`), 0o644); err != nil {
		t.Fatalf("plant outside vec: %v", err)
	}

	args := map[string]any{
		"scope":          scope,
		"query_vec_path": outside,
	}
	_, err := mcpSearch(args, scope)
	if err == nil {
		t.Fatalf("mcpSearch accepted an out-of-base query_vec_path; traversal not contained")
	}
	if strings.Contains(err.Error(), "LEAKED-CONTENTS") {
		t.Fatalf("mcpSearch leaked file contents in error: %v", err)
	}
}

func TestMCPSearch_QueryVecPath_AllowsInBase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := "vec-ok-" + filepath.Base(dir)
	ensureDirs(scope)

	// Plant an article + its vector so a legit in-base query vector resolves.
	stubArticle(t, scope, "a1", "Article One", "sum", "# A\n\nbody")
	idx := NewVectorIndex()
	idx.Add("a1", []float32{0.5, 0.5})
	if err := saveVectorIndex(scope, idx); err != nil {
		t.Fatalf("save vec index: %v", err)
	}

	// A query vector file inside the scope dir (under the kb base) is allowed.
	inBase := filepath.Join(scopeDir(scope), "qvec.json")
	if err := os.WriteFile(inBase, []byte(`[0.5,0.5]`), 0o644); err != nil {
		t.Fatalf("write in-base vec: %v", err)
	}

	args := map[string]any{
		"scope":          scope,
		"query_vec_path": inBase,
	}
	if _, err := mcpSearch(args, scope); err != nil {
		t.Fatalf("mcpSearch rejected an in-base query_vec_path: %v", err)
	}
}

func TestLoadVectorFromFile_ParseErrorDoesNotLeakContents(t *testing.T) {
	dir := t.TempDir()

	// A file whose bytes would otherwise be echoed by encoding/json's offset
	// errors. The containment fix must return a generic parse error that does
	// not dump the file body.
	bad := filepath.Join(dir, "bad.json")
	secretBody := "{SENSITIVE-INNER-BYTES-SHOULD-NOT-APPEAR"
	if err := os.WriteFile(bad, []byte(secretBody), 0o644); err != nil {
		t.Fatalf("write bad vec: %v", err)
	}

	_, err := loadVectorFromFile(bad)
	if err == nil {
		t.Fatalf("expected parse error on malformed vector file")
	}
	if strings.Contains(err.Error(), "SENSITIVE-INNER-BYTES") {
		t.Fatalf("parse error leaked file contents: %v", err)
	}
}
