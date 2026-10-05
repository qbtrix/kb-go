// In-process tests for the bring-your-own-compiler hook (`--compiler` /
// KB_COMPILER): running the hook, parsing its output, timeouts and stderr
// relay, flag resolution, the optional `usage` metadata, and the version
// string. The built-in Anthropic client is tested in anthropic_test.go;
// binary-level tests (every command, precedence, refusals) live in
// e2e_test.go.
//
// TestMain is kbtest.Main: it isolates the home directory for the package and,
// when KB_FAKE_COMPILER is set, turns the re-executed test binary into the fake
// compiler (kbtest.FakeCompiler); it also clears ANTHROPIC_API_KEY,
// ANTHROPIC_BASE_URL and KB_COMPILER so no test can reach a real API.

package main

import (
	"io"
	"os"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}

// --- Hook unit tests --------------------------------------------------------

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

// --- Commands with the hook -------------------------------------------------

func TestIngestCompilerFailureKeepsRawWritesNoArticle(t *testing.T) {
	kbtest.IsolatedHome(t)
	scope := "ing-fail"
	t.Setenv("KB_FAKE_COMPILER", "fail")
	spec := compile.Spec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: io.Discard}
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
