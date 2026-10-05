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

	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

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
