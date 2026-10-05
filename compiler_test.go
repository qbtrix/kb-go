// In-process tests for the caller-owned compiler hook (`--compiler` /
// KB_COMPILER): running the hook, parsing its output, timeouts and stderr
// relay, the optional `usage` metadata, the guarantee that kb itself holds no
// LLM client, and the version string. Binary-level hook tests live in
// e2e_test.go.
//
// TestMain is kbtest.Main: it isolates the home directory for the package and,
// when KB_FAKE_COMPILER is set, turns the re-executed test binary into the fake
// compiler (kbtest.FakeCompiler).

package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}

// --- Hook unit tests --------------------------------------------------------

func TestCompilerHookHappyPath(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "ok")
	spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second}

	art, err := compileWithHook(spec, "package main\nfunc main() {}\n", "cmd/app/main.go", nil, true)
	if err != nil {
		t.Fatalf("compileWithHook: %v", err)
	}
	if art.Title != "Fake cmd/app/main.go" {
		t.Errorf("title = %q (prompt Source line not delivered on stdin?)", art.Title)
	}
	if !strings.Contains(art.Content, "PROMPT_BYTES=") || strings.Contains(art.Content, "PROMPT_BYTES=0") {
		t.Errorf("prompt was not written to the compiler's stdin: %q", art.Content)
	}
	if art.Usage == nil || art.Usage.InputTokens != 100 || art.Usage.OutputTokens != 20 || art.Usage.CostUSD != 0.001 || art.Usage.Model != "fake-model" {
		t.Errorf("usage not parsed from hook output: %+v", art.Usage)
	}
	if art.CompiledWith != "fake-model" {
		t.Errorf("CompiledWith = %q, want usage.model %q", art.CompiledWith, "fake-model")
	}
	if art.Depth != "overview" || art.Audience != "agent" {
		t.Errorf("terse metadata not applied: depth=%q audience=%q", art.Depth, art.Audience)
	}
}

func TestCompilerHookShellQuoting(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "argv")
	spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, `"two words" plain`), Timeout: 60 * time.Second}
	art, err := compileWithHook(spec, "text", "doc.md", nil, false)
	if err != nil {
		t.Fatalf("compileWithHook: %v", err)
	}
	if !strings.Contains(art.Content, "ARGV=two words|plain") {
		t.Errorf("shell did not preserve the quoted argument: %q", art.Content)
	}
}

func TestCompilerHookDefaultCompiledWith(t *testing.T) {
	spec := compilerSpec{Command: `"/opt/bin/my-compiler.sh" --fast`}
	if got := spec.label(); got != "compiler:my-compiler.sh" {
		t.Errorf("label = %q, want compiler:my-compiler.sh", got)
	}
	if got := (compilerSpec{Command: "claude -p --tools \"\""}).label(); got != "compiler:claude" {
		t.Errorf("label = %q, want compiler:claude", got)
	}
}

func TestCompilerHookFencedOutputAndExtraKeys(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "fenced")
	spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second}
	art, err := compileWithHook(spec, "text", "doc.md", nil, false)
	if err != nil {
		t.Fatalf("fenced output should parse: %v", err)
	}
	if art.Title != "Fake doc.md" {
		t.Errorf("title = %q", art.Title)
	}
}

func TestCompilerHookNonZeroExitIsLoud(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "fail")
	var stderr bytes.Buffer
	spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: &stderr}
	art, err := compileWithHook(spec, "text", "notes/a.md", nil, false)
	if err == nil || art != nil {
		t.Fatalf("non-zero exit must fail with no article, got art=%v err=%v", art, err)
	}
	if !strings.Contains(err.Error(), "exit") {
		t.Errorf("error should mention the exit status: %v", err)
	}
	if !strings.Contains(stderr.String(), "model unavailable") || !strings.Contains(stderr.String(), "notes/a.md") {
		t.Errorf("compiler stderr should pass through prefixed with the source, got %q", stderr.String())
	}
}

func TestCompilerHookTimeoutIsLoud(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "sleep")
	spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 1 * time.Second, Stderr: io.Discard}
	start := time.Now()
	art, err := compileWithHook(spec, "text", "slow.md", nil, false)
	if err == nil || art != nil {
		t.Fatalf("timeout must fail with no article")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error should say timed out: %v", err)
	}
	if el := time.Since(start); el > 15*time.Second {
		t.Errorf("timeout did not stop the compiler promptly: %v", el)
	}
}

func TestCompilerHookGarbageOutputIsLoud(t *testing.T) {
	for _, mode := range []string{"garbage", "nocontent"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("KB_FAKE_COMPILER", mode)
			spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: io.Discard}
			art, err := compileWithHook(spec, "raw text that must never be stored verbatim", "x.md", nil, false)
			if err == nil || art != nil {
				t.Fatalf("%s output must fail with no article, got art=%+v", mode, art)
			}
		})
	}
}

func TestParseUsageLenient(t *testing.T) {
	u := parseUsage(json.RawMessage(`{"model":"m","input_tokens":12,"output_tokens":"bad","cost_usd":0.5,"extra":[1]}`))
	if u == nil || u.Model != "m" || u.InputTokens != 12 || u.OutputTokens != 0 || u.CostUSD != 0.5 {
		t.Errorf("parseUsage = %+v", u)
	}
	if parseUsage(nil) != nil || parseUsage(json.RawMessage(`null`)) != nil || parseUsage(json.RawMessage(`"x"`)) != nil {
		t.Errorf("absent/invalid usage should be nil")
	}
	if parseUsage(json.RawMessage(`{}`)) != nil {
		t.Errorf("empty usage object should be nil")
	}
}

func TestCompilerFromArgs(t *testing.T) {
	t.Setenv("KB_COMPILER", "env-cmd")
	spec, err := compilerFromArgs([]string{"x"})
	if err != nil || spec.Command != "env-cmd" || spec.Timeout != 300*time.Second {
		t.Errorf("env fallback / default timeout: %+v %v", spec, err)
	}
	spec, err = compilerFromArgs([]string{"--compiler", "flag-cmd", "--compiler-timeout", "45"})
	if err != nil || spec.Command != "flag-cmd" || spec.Timeout != 45*time.Second {
		t.Errorf("flag wins / int seconds: %+v %v", spec, err)
	}
	spec, err = compilerFromArgs([]string{"--compiler-timeout", "2m"})
	if err != nil || spec.Timeout != 2*time.Minute {
		t.Errorf("duration timeout: %+v %v", spec, err)
	}
	if _, err := compilerFromArgs([]string{"--compiler-timeout", "soon"}); err == nil {
		t.Errorf("bad timeout should error")
	}
}

// --- No LLM client in kb ---------------------------------------------------

func TestNoLLMClientInSources(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	banned := []string{"ANTHROPIC_API_KEY", "api.anthropic.com", "anthropic-version", "x-api-key"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range banned {
			if bytes.Contains(b, []byte(s)) {
				t.Errorf("%s still references %q: kb must not contain an LLM client", f, s)
			}
		}
	}
}

// --- Commands with the hook -------------------------------------------------

func TestIngestCompilerFailureKeepsRawWritesNoArticle(t *testing.T) {
	kbtest.IsolatedHome(t)
	scope := "ing-fail"
	t.Setenv("KB_FAKE_COMPILER", "fail")
	spec := compilerSpec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: io.Discard}
	text := "raw text that must not silently become an article"
	err := ingestText(scope, "notes.md", spec, "", "", text, false, false)
	if err == nil {
		t.Fatal("ingest must fail loudly when the compiler fails")
	}
	if !strings.Contains(err.Error(), textutil.ContentHash(text)[:16]) || !strings.Contains(err.Error(), "--allow-fallback") {
		t.Errorf("error should name the raw doc and the escape hatch: %v", err)
	}
	if rawDocCount(t, scope) != 1 || wikiArticleCount(t, scope) != 0 {
		t.Errorf("raw doc kept, no article: raw=%d wiki=%d", rawDocCount(t, scope), wikiArticleCount(t, scope))
	}
}

// --- usage metadata ---------------------------------------------------------

func TestArticleJSONStoresUsage(t *testing.T) {
	kbtest.IsolatedHome(t)
	scope := "aj-usage"
	payload := `{"raw_text":"r","article":{"title":"With Usage","content":"c","usage":{"model":"gpt-x","input_tokens":5,"output_tokens":6,"cost_usd":0.25}}}`
	if err := ingestArticleJSON(scope, []byte(payload), false); err != nil {
		t.Fatal(err)
	}
	a, _ := loadArticle(scope, "with-usage")
	if a == nil || a.Usage == nil || a.Usage.InputTokens != 5 || a.Usage.CostUSD != 0.25 {
		t.Fatalf("usage not stored: %+v", a)
	}
	if a.CompiledWith != "gpt-x" {
		t.Errorf("CompiledWith = %q, want usage.model", a.CompiledWith)
	}
	// An explicit compiled_with still wins.
	payload = `{"raw_text":"r2","article":{"title":"Explicit","content":"c","compiled_with":"pp-backend","usage":{"model":"gpt-x"}}}`
	ingestArticleJSON(scope, []byte(payload), false)
	b, _ := loadArticle(scope, "explicit")
	if b == nil || b.CompiledWith != "pp-backend" {
		t.Errorf("explicit compiled_with should win: %+v", b)
	}
}

// --- version ----------------------------------------------------------------

func TestFormatVersion(t *testing.T) {
	cases := []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"no build info", nil, false, "dev"},
		{"go install @v", &debug.BuildInfo{Main: debug.Module{Version: "v0.4.0"}}, true, "v0.4.0"},
		{"devel no vcs", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}, true, "dev"},
		{"devel with vcs", &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}, Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "cc38b2b0123456789abcdef"}, {Key: "vcs.modified", Value: "true"},
		}}, true, "dev (cc38b2b01234, dirty)"},
		{"empty version with vcs", &debug.BuildInfo{Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc"},
		}}, true, "dev (abc)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := formatVersion(c.info, c.ok); got != c.want {
				t.Errorf("formatVersion = %q, want %q", got, c.want)
			}
		})
	}
}
