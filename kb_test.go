// kb_test.go — Tests for the CLI-level pieces still in package main:
// structural lint, file scanning, the concept graph (build and mermaid
// render), `build --since` (changedFilesSinceRef against a scratch git repo),
// and category normalisation.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// --- Structural Lint ---

func TestLintEmptyKB(t *testing.T) {
	scope := "test-lint-empty-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()
	store.EnsureDirs(scope)

	issues := lintStructural(scope)
	if len(issues) == 0 {
		t.Error("expected warning for empty KB")
	}
	if issues[0].Type != "gap" {
		t.Errorf("expected 'gap' issue, got %q", issues[0].Type)
	}
}

func TestLintMissingConcepts(t *testing.T) {
	scope := "test-lint-concepts-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	article := &model.WikiArticle{
		ID:       "test",
		Title:    "Test",
		Summary:  "A test",
		Content:  "Some content",
		Concepts: []string{}, // empty
		Version:  1,
	}
	store.SaveArticle(scope, article)

	issues := lintStructural(scope)
	found := false
	for _, issue := range issues {
		if issue.Type == "gap" && strings.Contains(issue.Message, "no concepts") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing concepts")
	}
}

func TestLintBrokenBacklink(t *testing.T) {
	scope := "test-lint-backlink-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	article := &model.WikiArticle{
		ID:        "test",
		Title:     "Test",
		Summary:   "A test",
		Content:   "Content",
		Concepts:  []string{"test"},
		Backlinks: []string{"nonexistent"},
		Version:   1,
	}
	store.SaveArticle(scope, article)

	issues := lintStructural(scope)
	found := false
	for _, issue := range issues {
		if issue.Type == "connection" && strings.Contains(issue.Message, "broken backlink") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about broken backlink")
	}
}

// --- File Scanning ---

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("# main"), 0o644)
	os.WriteFile(filepath.Join(dir, "test.py"), []byte("# test"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("git"), 0o644)

	files := scanDir(dir, "*.py")
	if len(files) != 2 {
		t.Errorf("expected 2 .py files, got %d: %v", len(files), files)
	}

	// Should skip .git
	allFiles := scanDir(dir, "*")
	for _, f := range allFiles {
		if strings.Contains(f, ".git") {
			t.Error("scanDir should skip .git directory")
		}
	}
}

func TestScanDirSkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte("//"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("// app"), 0o644)

	files := scanDir(dir, "*.js")
	if len(files) != 1 {
		t.Errorf("expected 1 file (skipping node_modules), got %d: %v", len(files), files)
	}
}

func TestScanDirMultiPattern(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("// go"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.py"), []byte("# python"), 0o644)
	os.WriteFile(filepath.Join(dir, "index.ts"), []byte("// ts"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)

	files := scanDir(dir, "*.go,*.py,*.ts")
	if len(files) != 3 {
		t.Errorf("expected 3 files with multi-pattern, got %d: %v", len(files), files)
	}
}

// --- Graph Export ---

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
	nodes, edges := buildConceptGraph(idx, 10, 2)
	if len(nodes) != 3 {
		t.Errorf("nodes = %d, want 3 (orphan should be filtered)", len(nodes))
	}
	// auth-jwt share a1,a2 ; auth-session share a2,a3 ; jwt-session share a2
	if len(edges) != 3 {
		t.Errorf("edges = %d, want 3", len(edges))
	}
}

func TestRenderMermaid(t *testing.T) {
	nodes := []graphNode{
		{ID: "c0", Label: "auth", Kind: "concept", Size: 3},
		{ID: "c1", Label: "jwt", Kind: "concept", Size: 2},
	}
	edges := []graphEdge{{Source: "c0", Target: "c1", Weight: 2}}
	out := renderMermaid(nodes, edges, "")
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

// --- --since flag ---

// initGitRepo initialises a bare git repo in dir so we can commit files.
// Returns an error if git is not available on PATH.
func initGitRepo(t *testing.T, dir string) error {
	t.Helper()
	cmds := [][]string{
		{"git", "init", dir},
		{"git", "-C", dir, "config", "user.email", "test@example.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, c := range cmds {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
	}
	return nil
}

// gitCommitAll stages and commits all files in dir with the given message.
func gitCommitAll(t *testing.T, dir, msg string) error {
	t.Helper()
	cmds := [][]string{
		{"git", "-C", dir, "add", "."},
		{"git", "-C", dir, "commit", "-m", msg},
	}
	for _, c := range cmds {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%v: %s", err, out)
		}
	}
	return nil
}

// TestChangedFilesSinceRef unit-tests the helper on a crafted git repo.
func TestChangedFilesSinceRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	if err := initGitRepo(t, dir); err != nil {
		t.Fatalf("initGitRepo: %v", err)
	}

	// Write and commit two files.
	os.WriteFile(filepath.Join(dir, "alpha.py"), []byte("# alpha v1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "beta.py"), []byte("# beta v1\n"), 0o644)
	if err := gitCommitAll(t, dir, "initial"); err != nil {
		t.Fatalf("commit initial: %v", err)
	}

	// Modify only alpha.
	os.WriteFile(filepath.Join(dir, "alpha.py"), []byte("# alpha v2\n"), 0o644)

	changed, err := changedFilesSinceRef(dir, "HEAD")
	if err != nil {
		t.Fatalf("changedFilesSinceRef: %v", err)
	}

	if !changed["alpha.py"] {
		t.Error("expected alpha.py in changed set")
	}
	if changed["beta.py"] {
		t.Error("beta.py should NOT be in changed set (not modified)")
	}
}

// TestBuildSinceRefSkipsUnchanged verifies that --since filters unchanged files.
// We use cmdPrepare (no API key required) which follows the same scan+cache path.
func TestBuildSinceRefSkipsUnchanged(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	if err := initGitRepo(t, dir); err != nil {
		t.Fatalf("initGitRepo: %v", err)
	}

	// Two .py files, both committed.
	os.WriteFile(filepath.Join(dir, "alpha.py"), []byte("# alpha v1\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "beta.py"), []byte("# beta v1\n"), 0o644)
	if err := gitCommitAll(t, dir, "initial"); err != nil {
		t.Fatalf("commit initial: %v", err)
	}

	// Modify only alpha (unstaged — git diff HEAD shows it as modified).
	os.WriteFile(filepath.Join(dir, "alpha.py"), []byte("# alpha v2\n"), 0o644)

	scope := "test-since-" + textutil.ContentHash(dir)[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	// Capture stdout from cmdPrepare.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmdPrepare([]string{dir, "--scope", scope, "--pattern", "*.py", "--since", "HEAD"})

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)

	var out struct {
		Pending int `json:"pending"`
		Cached  int `json:"cached"`
		Total   int `json:"total"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse prepare output: %v\nraw: %s", err, buf.String())
	}

	if out.Total != 2 {
		t.Errorf("total = %d, want 2", out.Total)
	}
	// Only alpha.py is in the changed set, so only 1 item should be pending.
	if out.Pending != 1 {
		t.Errorf("pending = %d, want 1 (only the modified file)", out.Pending)
	}
	if out.Cached != 1 {
		t.Errorf("cached (since-filtered) = %d, want 1 (unchanged beta.py)", out.Cached)
	}
}

// TestBuildSinceNonGitFallback verifies that a non-git path triggers a warning
// on stderr and falls back to a full build (all files pending).
func TestBuildSinceNonGitFallback(t *testing.T) {
	dir := t.TempDir() // plain directory, not a git repo

	os.WriteFile(filepath.Join(dir, "main.py"), []byte("# main\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "util.py"), []byte("# util\n"), 0o644)

	scope := "test-since-fallback-" + textutil.ContentHash(dir)[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	// Capture stderr for the warning.
	origStderr := os.Stderr
	sr, sw, _ := os.Pipe()
	os.Stderr = sw

	// Capture stdout so cmdPrepare JSON doesn't bleed.
	origStdout := os.Stdout
	or, ow, _ := os.Pipe()
	os.Stdout = ow

	cmdPrepare([]string{dir, "--scope", scope, "--pattern", "*.py", "--since", "HEAD"})

	sw.Close()
	ow.Close()
	os.Stderr = origStderr
	os.Stdout = origStdout

	var stderrBuf, stdoutBuf bytes.Buffer
	stderrBuf.ReadFrom(sr)
	stdoutBuf.ReadFrom(or)

	stderrStr := stderrBuf.String()
	if !strings.Contains(stderrStr, "falling back to full build") {
		t.Errorf("expected fallback warning in stderr, got: %q", stderrStr)
	}

	var out struct {
		Pending int `json:"pending"`
		Total   int `json:"total"`
	}
	if err := json.Unmarshal(stdoutBuf.Bytes(), &out); err != nil {
		t.Fatalf("failed to parse prepare output: %v\nraw: %s", err, stdoutBuf.String())
	}
	// Full build: all files included.
	if out.Pending != 2 {
		t.Errorf("pending = %d, want 2 (full fallback build)", out.Pending)
	}
	if out.Total != 2 {
		t.Errorf("total = %d, want 2", out.Total)
	}
}

// TestChangedFilesSinceRefRejectsOptionLikeRef verifies that a ref value
// starting with "-" (which would otherwise be interpreted as a git option
// on the diff call) is rejected at the rev-parse stage.
func TestChangedFilesSinceRefRejectsOptionLikeRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	if err := initGitRepo(t, dir); err != nil {
		t.Fatalf("initGitRepo: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "one.py"), []byte("# one\n"), 0o644)
	if err := gitCommitAll(t, dir, "initial"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// A ref that looks like a git option must be rejected by rev-parse,
	// not forwarded to git diff where it would be interpreted as a flag.
	for _, badRef := range []string{"--upload-pack=evil", "-p", "--help"} {
		_, err := changedFilesSinceRef(dir, badRef)
		if err == nil {
			t.Errorf("changedFilesSinceRef(%q) should reject option-like ref", badRef)
		}
	}
}

// TestChangedFilesSinceRefNonexistentRef verifies that a ref that doesn't
// exist in the repo produces an error so the caller can fall back to a
// full build.
func TestChangedFilesSinceRefNonexistentRef(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	dir := t.TempDir()
	if err := initGitRepo(t, dir); err != nil {
		t.Fatalf("initGitRepo: %v", err)
	}
	os.WriteFile(filepath.Join(dir, "one.py"), []byte("# one\n"), 0o644)
	if err := gitCommitAll(t, dir, "initial"); err != nil {
		t.Fatalf("commit: %v", err)
	}

	_, err := changedFilesSinceRef(dir, "no-such-ref-exists-here")
	if err == nil {
		t.Error("changedFilesSinceRef should fail on nonexistent ref")
	}
}

// --- Category normalization ---

func TestNormalizeCategory(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"CLI", "cli"},
		{"CLI tool", "cli tool"},
		{"cli tool", "cli tool"},
		{"  cli  tool  ", "cli tool"},
		{"Database.", "database"},
		{"Storage--", "storage"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := normalizeCategory(tt.in); got != tt.want {
			t.Errorf("normalizeCategory(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPickCanonicalVariant(t *testing.T) {
	// Count dominates
	got := pickCanonicalVariant(map[string]int{"cli": 10, "CLI": 2, "CLI tool": 1})
	if got != "cli" {
		t.Errorf("expected 'cli' (highest count), got %q", got)
	}
	// Count tie → shortest wins
	got = pickCanonicalVariant(map[string]int{"database": 3, "Database engine": 3})
	if got != "database" {
		t.Errorf("expected 'database' (shortest on tie), got %q", got)
	}
	// All tied on count + length, all already-normalized → alphabetical
	got = pickCanonicalVariant(map[string]int{"zebra": 1, "alpha": 1, "bravo": 1})
	if got != "alpha" {
		t.Errorf("expected 'alpha' (alphabetical fallback), got %q", got)
	}
	// Tied on count + length, one matches its own normalize() → clean form wins
	got = pickCanonicalVariant(map[string]int{"Storage": 1, "storage": 1})
	if got != "storage" {
		t.Errorf("expected 'storage' (clean form preferred on tie), got %q", got)
	}
}

func TestApplyCategoryCanonical(t *testing.T) {
	scope := "test-norm-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	articles := []*model.WikiArticle{
		{ID: "a1", Title: "A1", Content: "x", Categories: []string{"CLI", "storage"}, Version: 1},
		{ID: "a2", Title: "A2", Content: "x", Categories: []string{"cli", "Storage"}, Version: 1},
		{ID: "a3", Title: "A3", Content: "x", Categories: []string{"cli tool", "database"}, Version: 1},
		{ID: "a4", Title: "A4", Content: "x", Categories: []string{"cli"}, Version: 1},
	}
	for _, a := range articles {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	// Build clusters manually mirroring runCategoryNormalize's logic so the
	// test exercises applyCategoryCanonical in isolation.
	all, _ := store.ListArticles(scope)
	clusterMap := map[string]*categoryCluster{}
	for _, a := range all {
		for _, cat := range a.Categories {
			key := normalizeCategory(cat)
			c, ok := clusterMap[key]
			if !ok {
				c = &categoryCluster{Key: key, Variants: map[string]int{}}
				clusterMap[key] = c
			}
			c.Variants[cat]++
			c.Total++
		}
	}
	var noisy []*categoryCluster
	for _, c := range clusterMap {
		if len(c.Variants) > 1 {
			c.Canonical = pickCanonicalVariant(c.Variants)
			noisy = append(noisy, c)
		}
	}

	changed := applyCategoryCanonical(scope, all, noisy)
	if changed == 0 {
		t.Fatal("expected at least one article rewritten")
	}

	// Verify: after apply, only canonical variants remain from multi-variant clusters.
	// Cluster "cli" has variants {CLI, cli} → canonical "cli" (ASCII sort tie; "cli" is clean form)
	// Cluster "storage" has variants {storage, Storage} → canonical "storage" (clean form preferred on tie)
	// "cli tool" and "database" are singleton clusters — not noisy, unchanged.
	after, _ := store.ListArticles(scope)
	seenVariants := map[string]bool{}
	for _, a := range after {
		for _, cat := range a.Categories {
			seenVariants[cat] = true
		}
	}
	for _, bad := range []string{"CLI", "Storage"} {
		if seenVariants[bad] {
			t.Errorf("%q should have been canonicalized out", bad)
		}
	}
	for _, good := range []string{"cli", "storage", "cli tool", "database"} {
		if !seenVariants[good] {
			t.Errorf("expected %q to remain after normalization", good)
		}
	}
}

func TestApplyCategoryCanonicalDedupesCollapsed(t *testing.T) {
	// Article with ["CLI", "cli"] should collapse to ["cli"] — no dupes.
	scope := "test-norm-dedup-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	a := &model.WikiArticle{
		ID:         "d1",
		Title:      "D1",
		Content:    "x",
		Categories: []string{"CLI", "cli"},
		Version:    1,
	}
	if err := store.SaveArticle(scope, a); err != nil {
		t.Fatalf("save: %v", err)
	}
	all, _ := store.ListArticles(scope)
	noisy := []*categoryCluster{
		{
			Key:       "cli",
			Variants:  map[string]int{"CLI": 1, "cli": 1},
			Total:     2,
			Canonical: "cli",
		},
	}
	if changed := applyCategoryCanonical(scope, all, noisy); changed != 1 {
		t.Errorf("expected 1 article changed, got %d", changed)
	}
	loaded, _ := store.LoadArticle(scope, "d1")
	if len(loaded.Categories) != 1 || loaded.Categories[0] != "cli" {
		t.Errorf("categories = %v, want [cli]", loaded.Categories)
	}
}

func TestApplyCategoryPersistsIndex(t *testing.T) {
	// Regression: after --apply rewrites categories, the on-disk BM25 index
	// must reflect the new category set. Earlier version built the index in
	// memory but forgot to save it, so `kb stats` kept showing stale counts.
	scope := "test-norm-idx-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	articles := []*model.WikiArticle{
		{ID: "a1", Title: "A1", Content: "x", Categories: []string{"CLI"}, Version: 1},
		{ID: "a2", Title: "A2", Content: "x", Categories: []string{"cli"}, Version: 1},
	}
	for _, a := range articles {
		if err := store.SaveArticle(scope, a); err != nil {
			t.Fatalf("save: %v", err)
		}
	}
	// Initial index with both variants
	all, _ := store.ListArticles(scope)
	_ = store.SaveIndex(scope, store.RebuildIndex(scope, all))

	noisy := []*categoryCluster{{
		Key:       "cli",
		Variants:  map[string]int{"CLI": 1, "cli": 1},
		Canonical: "cli",
	}}
	if changed := applyCategoryCanonical(scope, all, noisy); changed == 0 {
		t.Fatal("expected apply to rewrite")
	}

	// Load the persisted index from disk and verify it reflects the collapse.
	data, err := os.ReadFile(filepath.Join(store.ScopeDir(scope), "index.json"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	var persisted model.KnowledgeIndex
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("parse index: %v", err)
	}
	if len(persisted.Categories) != 1 {
		t.Errorf("persisted categories = %v, want [cli] only", persisted.Categories)
	}
	if persisted.Categories[0] != "cli" {
		t.Errorf("persisted category = %q, want 'cli'", persisted.Categories[0])
	}
}

func TestAffectedArticleCount(t *testing.T) {
	articles := []*model.WikiArticle{
		{ID: "a1", Categories: []string{"cli"}},            // already canonical
		{ID: "a2", Categories: []string{"CLI"}},            // needs rewrite
		{ID: "a3", Categories: []string{"storage"}},        // not in clusters
		{ID: "a4", Categories: []string{"cli", "Storage"}}, // one needs rewrite
	}
	clusters := []*categoryCluster{
		{
			Key:       "cli",
			Variants:  map[string]int{"cli": 10, "CLI": 1},
			Canonical: "cli",
		},
	}
	got := affectedArticleCount(articles, clusters)
	if got != 1 {
		t.Errorf("affectedArticleCount = %d, want 1", got)
	}
}
