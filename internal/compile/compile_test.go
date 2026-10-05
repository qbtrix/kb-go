// In-process tests for the compiler hook: the happy path, shell quoting, the
// default compiled_with label, fenced output with extra keys, loud failures
// (non-zero exit, timeout, garbage output), lenient usage parsing, and the
// terse prompt target.
//
// The compiler under test is this test binary re-executed: TestMain is
// kbtest.Main, which acts as the fake compiler (kbtest.FakeCompiler) when
// KB_FAKE_COMPILER is set, so the command goes through the real platform shell.
// kbtest.Main also clears ANTHROPIC_API_KEY, ANTHROPIC_BASE_URL and
// KB_COMPILER, so no test can reach a real API.

package compile

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

func TestCompilerHookHappyPath(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "ok")
	spec := Spec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second}

	art, err := hookArticle(spec, "package main\nfunc main() {}\n", "cmd/app/main.go", "", true)
	if err != nil {
		t.Fatalf("hookArticle: %v", err)
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
	spec := Spec{Command: kbtest.FakeCompilerCommand(t, `"two words" plain`), Timeout: 60 * time.Second}
	art, err := hookArticle(spec, "text", "doc.md", "", false)
	if err != nil {
		t.Fatalf("hookArticle: %v", err)
	}
	if !strings.Contains(art.Content, "ARGV=two words|plain") {
		t.Errorf("shell did not preserve the quoted argument: %q", art.Content)
	}
}

func TestCompilerHookDefaultCompiledWith(t *testing.T) {
	spec := Spec{Command: `"/opt/bin/my-compiler.sh" --fast`}
	if got := spec.Label(); got != "compiler:my-compiler.sh" {
		t.Errorf("label = %q, want compiler:my-compiler.sh", got)
	}
	if got := (Spec{Command: "claude -p --tools \"\""}).Label(); got != "compiler:claude" {
		t.Errorf("label = %q, want compiler:claude", got)
	}
}

func TestCompilerHookFencedOutputAndExtraKeys(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "fenced")
	spec := Spec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second}
	art, err := hookArticle(spec, "text", "doc.md", "", false)
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
	spec := Spec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: &stderr}
	art, err := hookArticle(spec, "text", "notes/a.md", "", false)
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
	spec := Spec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 1 * time.Second, Stderr: io.Discard}
	start := time.Now()
	art, err := hookArticle(spec, "text", "slow.md", "", false)
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
			spec := Spec{Command: kbtest.FakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: io.Discard}
			art, err := hookArticle(spec, "raw text that must never be stored verbatim", "x.md", "", false)
			if err == nil || art != nil {
				t.Fatalf("%s output must fail with no article, got art=%+v", mode, art)
			}
		})
	}
}

func TestParseUsageLenient(t *testing.T) {
	u := ParseUsage(json.RawMessage(`{"model":"m","input_tokens":12,"output_tokens":"bad","cost_usd":0.5,"extra":[1]}`))
	if u == nil || u.Model != "m" || u.InputTokens != 12 || u.OutputTokens != 0 || u.CostUSD != 0.5 {
		t.Errorf("parseUsage = %+v", u)
	}
	if ParseUsage(nil) != nil || ParseUsage(json.RawMessage(`null`)) != nil || ParseUsage(json.RawMessage(`"x"`)) != nil {
		t.Errorf("absent/invalid usage should be nil")
	}
	if ParseUsage(json.RawMessage(`{}`)) != nil {
		t.Errorf("empty usage object should be nil")
	}
}

// TestCompilePromptTerseModeShorterTarget confirms Prompt varies
// the word-count target based on the terse flag.
func TestCompilePromptTerseModeShorterTarget(t *testing.T) {
	tersePrompt := Prompt("src/main.go", "", "// some code", true)
	defaultPrompt := Prompt("src/main.go", "", "// some code", false)

	if !strings.Contains(tersePrompt, "120-180 words") {
		t.Errorf("terse prompt should contain '120-180 words', got:\n%s", tersePrompt)
	}
	if strings.Contains(tersePrompt, "400-800") {
		t.Errorf("terse prompt should not mention '400-800' word range")
	}

	if !strings.Contains(defaultPrompt, "400-800 words") {
		t.Errorf("default prompt should contain '400-800 words', got:\n%s", defaultPrompt)
	}
	if strings.Contains(defaultPrompt, "120-180") {
		t.Errorf("default prompt should not mention terse range '120-180'")
	}
}

func TestMain(m *testing.M) {
	os.Exit(kbtest.Main(m))
}
