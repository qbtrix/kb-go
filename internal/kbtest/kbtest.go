// Package kbtest holds the test plumbing every kb-go package shares: home
// isolation, repo-root fixture paths, the kb binary for end-to-end tests, and
// the fake compiler the --compiler hook tests re-execute.
//
// Invariants:
//   - Every test package's TestMain goes through Main, so no test can write
//     into the real ~/.knowledge-base: Main points HOME and USERPROFILE (which
//     os.UserHomeDir reads on Windows) at a throwaway directory before any
//     test runs. Tests that need their own home call SetHome / IsolatedHome,
//     which always set both variables.
//   - Fixture paths (testdata/, examples/, benchmarks/) resolve from the
//     module root, so a test reads the same file from any package directory.
//   - Only the standard library and test-only code: kbtest must never import
//     internal/store (or anything above it), so every package can use it.
package kbtest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Module is the module path whose go.mod marks the repo root.
const Module = "github.com/qbtrix/kb-go"

// FakeCompilerEnv selects FakeCompiler mode when set in a test binary's env.
const FakeCompilerEnv = "KB_FAKE_COMPILER"

// Main is the shared TestMain body: os.Exit(kbtest.Main(m)). When the test
// binary was re-executed as a fake compiler (FakeCompilerEnv set) it behaves
// as that compiler instead of running tests. Otherwise it isolates the home
// directory for the whole process, runs the tests, and removes it.
func Main(m *testing.M) int {
	if mode := os.Getenv(FakeCompilerEnv); mode != "" {
		return FakeCompiler(mode)
	}
	home, err := os.MkdirTemp("", "kb-test-home-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "kbtest: temp home:", err)
		return 1
	}
	os.Setenv("HOME", home)
	os.Setenv("USERPROFILE", home)
	code := m.Run()
	os.RemoveAll(home)
	return code
}

// SetHome points HOME and USERPROFILE at dir for the rest of the test.
func SetHome(t testing.TB, dir string) {
	t.Helper()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// IsolatedHome gives the test a fresh empty home directory and returns it.
func IsolatedHome(t testing.TB) string {
	t.Helper()
	dir := t.TempDir()
	SetHome(t, dir)
	return dir
}

var (
	rootOnce sync.Once
	rootDir  string
)

// RepoRoot returns the module root: the nearest directory at or above the
// working directory whose go.mod declares Module. Empty if there is none.
func RepoRoot() string {
	rootOnce.Do(func() {
		dir, err := os.Getwd()
		if err != nil {
			return
		}
		for {
			b, err := os.ReadFile(filepath.Join(dir, "go.mod"))
			if err == nil && strings.Contains(strings.ReplaceAll(string(b), "\r", ""), "module "+Module+"\n") {
				rootDir = dir
				return
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				return
			}
			dir = parent
		}
	})
	return rootDir
}

// RootPath joins rel (slash-separated, e.g. "testdata/x.md") onto the module
// root. If the root cannot be found it returns rel unchanged.
func RootPath(rel string) string {
	root := RepoRoot()
	if root == "" {
		return rel
	}
	return filepath.Join(root, filepath.FromSlash(rel))
}

// Path is RootPath for path elements, failing the test when there is no root.
func Path(t testing.TB, elem ...string) string {
	t.Helper()
	root := RepoRoot()
	if root == "" {
		t.Fatalf("kbtest: module root (go.mod for %s) not found", Module)
	}
	return filepath.Join(append([]string{root}, elem...)...)
}

var (
	binMu   sync.Mutex
	binPath string
)

// BuildBinary compiles the kb binary from the module root once per test
// process and returns its path, for end-to-end tests that exec it.
func BuildBinary(t testing.TB) string {
	t.Helper()
	binMu.Lock()
	defer binMu.Unlock()
	if binPath != "" {
		if _, err := os.Stat(binPath); err == nil {
			return binPath
		}
	}
	dir, err := os.MkdirTemp("", "kb-test-bin-*")
	if err != nil {
		t.Fatalf("mkdtemp: %v", err)
	}
	path := filepath.Join(dir, "kb")
	if runtime.GOOS == "windows" {
		path += ".exe" // exec.Command cannot run an extensionless binary on Windows
	}
	cmd := exec.Command("go", "build", "-o", path, ".")
	cmd.Dir = Path(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	binPath = path
	return path
}

// FakeCompilerCommand returns a shell command line that re-executes the
// current test binary as a compiler, plus extra args appended verbatim. The
// caller sets FakeCompilerEnv to pick the mode.
func FakeCompilerCommand(t testing.TB, extra string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	cmd := `"` + exe + `"`
	if extra != "" {
		cmd += " " + extra
	}
	return cmd
}

// FakeCompiler is the compiler side of the re-exec trick: it reads the
// prompt on stdin and answers per mode, returning the exit code. Modes:
//
//	ok           article JSON titled after the prompt's "Source:" line, with usage
//	fenced       same, wrapped in ```json fences with extra keys
//	argv         content echoes os.Args[1:] (shell quoting check)
//	fail         writes to stderr, exits 3
//	fail-on:<s>  like ok, but fails when the source contains <s>
//	garbage      prints non-JSON text
//	nocontent    valid JSON without content
//	sleep        sleeps 30s (timeout check)
//	lint         prints a JSON array of one lint issue
func FakeCompiler(mode string) int {
	in, _ := io.ReadAll(os.Stdin)
	prompt := string(in)
	source := "unknown"
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "Source: ") {
			source = strings.TrimSpace(strings.TrimPrefix(line, "Source: "))
			break
		}
	}
	article := map[string]any{
		"title":      "Fake " + source,
		"summary":    "Compiled by the fake compiler.",
		"content":    fmt.Sprintf("Fake article for %s. PROMPT_BYTES=%d", source, len(prompt)),
		"concepts":   []string{"fake", "hook"},
		"categories": []string{"Testing"},
		"usage": map[string]any{
			"model": "fake-model", "input_tokens": 100, "output_tokens": 20, "cost_usd": 0.001,
			"ignored_extra": "x",
		},
	}
	switch {
	case mode == "ok":
	case mode == "fenced":
		b, _ := json.Marshal(article)
		fmt.Printf("```json\n%s\n```\n", b)
		return 0
	case mode == "argv":
		article["content"] = "ARGV=" + strings.Join(os.Args[1:], "|")
	case mode == "fail":
		fmt.Fprintln(os.Stderr, "fake compiler: model unavailable")
		return 3
	case strings.HasPrefix(mode, "fail-on:"):
		if strings.Contains(source, strings.TrimPrefix(mode, "fail-on:")) {
			fmt.Fprintln(os.Stderr, "fake compiler: refusing "+source)
			return 4
		}
	case mode == "garbage":
		fmt.Println("I am sorry, I cannot produce JSON today.")
		return 0
	case mode == "nocontent":
		fmt.Println(`{"title":"No Body","summary":"s"}`)
		return 0
	case mode == "sleep":
		time.Sleep(30 * time.Second)
		return 0
	case mode == "lint":
		fmt.Println(`[{"type":"gap","severity":"warning","message":"FAKE_LINT_ISSUE","suggestion":"write more"}]`)
		return 0
	}
	b, _ := json.Marshal(article)
	fmt.Println(string(b))
	return 0
}
