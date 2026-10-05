// Tests for the caller-owned compiler hook (`--compiler` / KB_COMPILER), the
// commands that require it, the optional `usage` metadata, and the guarantee
// that kb itself holds no LLM client.
//
// The compiler under test is this test binary re-executed: TestMain checks
// KB_FAKE_COMPILER and, when set, behaves as a compiler (reads the prompt on
// stdin, prints one article JSON object) instead of running the suite. That
// keeps the hook tests hermetic and cross-platform: the command string goes
// through the real platform shell (sh -c / cmd /S /C) exactly as a user's
// command would.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("KB_FAKE_COMPILER"); mode != "" {
		os.Exit(runFakeCompiler(mode))
	}
	os.Exit(m.Run())
}

// runFakeCompiler is the compiler side of the re-exec trick. Modes:
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
func runFakeCompiler(mode string) int {
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

// fakeCompilerCommand returns a shell command line that re-executes this test
// binary as a compiler, plus extra args appended verbatim.
func fakeCompilerCommand(t *testing.T, extra string) string {
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

// isolatedHome points both HOME and USERPROFILE at a temp dir (os.UserHomeDir
// reads USERPROFILE on Windows), so in-process and exec'd kb share a scratch
// knowledge base.
func isolatedHome(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
}

// runKB executes the built kb binary with extra env and optional stdin.
func runKB(t *testing.T, env []string, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	// A hard deadline turns a command that wrongly blocks (e.g. watch without a
	// compiler) into a test failure instead of a hung suite.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, buildTestBinary(t), args...)
	cmd.Env = append(os.Environ(), env...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	code = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else {
			t.Fatalf("run kb: %v", err)
		}
	}
	return o.String(), e.String(), code
}

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// --- Hook unit tests --------------------------------------------------------

func TestCompilerHookHappyPath(t *testing.T) {
	t.Setenv("KB_FAKE_COMPILER", "ok")
	spec := compilerSpec{Command: fakeCompilerCommand(t, ""), Timeout: 60 * time.Second}

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
	spec := compilerSpec{Command: fakeCompilerCommand(t, `"two words" plain`), Timeout: 60 * time.Second}
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
	spec := compilerSpec{Command: fakeCompilerCommand(t, ""), Timeout: 60 * time.Second}
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
	spec := compilerSpec{Command: fakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: &stderr}
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
	spec := compilerSpec{Command: fakeCompilerCommand(t, ""), Timeout: 1 * time.Second, Stderr: io.Discard}
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
			spec := compilerSpec{Command: fakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: io.Discard}
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

// --- Commands without a compiler: exit 2 with guidance ----------------------

func TestCommandsWithoutCompilerExit2(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "# A\nalpha", "b.md": "# B\nbeta"})
	// A key in the environment must change nothing: kb never reads it.
	env := []string{"KB_COMPILER=", "ANTHROPIC_API_KEY=sk-should-be-ignored"}

	cases := []struct {
		name  string
		stdin string
		args  []string
		want  []string
	}{
		{"build", "", []string{"build", src, "--scope", "nocomp", "--pattern", "*.md"}, []string{"--compiler", "kb prepare", "kb accept"}},
		{"ingest", "some text", []string{"ingest", "--scope", "nocomp"}, []string{"--compiler", "--article-json", "--allow-fallback"}},
		{"recompile", "", []string{"recompile", "--all", "--scope", "nocomp"}, []string{"--compiler"}},
		{"watch", "", []string{"watch", src, "--scope", "nocomp", "--pattern", "*.md"}, []string{"--compiler"}},
		{"lint-llm", "", []string{"lint", "--llm", "--scope", "nocomp"}, []string{"--compiler"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, stderr, code := runKB(t, env, c.stdin, c.args...)
			if code != 2 {
				t.Fatalf("exit code = %d, want 2; stderr: %s", code, stderr)
			}
			for _, w := range c.want {
				if !strings.Contains(stderr, w) {
					t.Errorf("guidance should mention %q, got: %s", w, stderr)
				}
			}
		})
	}
	if n := wikiArticleCount(t, "nocomp"); n != 0 {
		t.Errorf("no articles may be written without a compiler, got %d", n)
	}
	if n := rawDocCount(t, "nocomp"); n != 0 {
		t.Errorf("refusal must happen before any raw doc is written, got %d", n)
	}
}

func TestRemovedModelFlagIsRejected(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha"})
	_, stderr, code := runKB(t, nil, "", "build", src, "--scope", "m", "--pattern", "*.md", "--model", "claude-haiku")
	if code != 2 || !strings.Contains(stderr, "--model") || !strings.Contains(stderr, "--compiler") {
		t.Errorf("--model should be rejected with exit 2 and point at --compiler; code=%d stderr=%s", code, stderr)
	}
}

func TestBuildGlossaryOnlyNeedsNoCompiler(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{
		"glossary/pocket.md": "---\n{\"id\":\"pocket\",\"title\":\"Pocket\",\"kind\":\"glossary\",\"term\":\"Pocket\"}\n---\n\nA Pocket is a workspace.",
	})
	_, stderr, code := runKB(t, []string{"KB_COMPILER="}, "", "build", src, "--scope", "gl", "--pattern", "*.md")
	if code != 0 {
		t.Fatalf("glossary-only build needs no compiler; code=%d stderr=%s", code, stderr)
	}
}

// --- Commands with the hook -------------------------------------------------

func TestBuildWithCompilerHook(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "# A\nalpha", "b.md": "# B\nbeta"})
	env := []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=" + fakeCompilerCommand(t, "")}

	out, stderr, code := runKB(t, env, "", "build", src, "--scope", "hook", "--pattern", "*.md", "--json", "--concurrency", "2")
	if code != 0 {
		t.Fatalf("build failed: code=%d stderr=%s", code, stderr)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("build --json: %v\n%s", err, out)
	}
	if res["changed"].(float64) != 2 || res["input_tokens"].(float64) != 200 || res["output_tokens"].(float64) != 20*2 {
		t.Errorf("build json = %v", res)
	}
	a, err := loadArticle("hook", slugify("Fake a.md"))
	if err != nil || a == nil {
		t.Fatalf("article for a.md missing: %v", err)
	}
	if a.Usage == nil || a.Usage.InputTokens != 100 || a.CompiledWith != "fake-model" {
		t.Errorf("usage/compiled_with not stored: %+v %q", a.Usage, a.CompiledWith)
	}

	// Second build: everything cached, compiler not needed.
	out, stderr, code = runKB(t, []string{"KB_COMPILER="}, "", "build", src, "--scope", "hook", "--pattern", "*.md", "--json")
	if code != 0 || !strings.Contains(out, `"cached": 2`) {
		t.Errorf("cached rebuild should need no compiler: code=%d out=%s stderr=%s", code, out, stderr)
	}

	// Stats sums usage.
	out, _, code = runKB(t, nil, "", "stats", "--scope", "hook", "--json")
	if code != 0 {
		t.Fatalf("stats failed")
	}
	var st struct {
		Usage *struct {
			Articles     int     `json:"articles"`
			InputTokens  int     `json:"input_tokens"`
			OutputTokens int     `json:"output_tokens"`
			CostUSD      float64 `json:"cost_usd"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(out), &st); err != nil || st.Usage == nil {
		t.Fatalf("stats --json should carry usage totals: %v\n%s", err, out)
	}
	if st.Usage.Articles != 2 || st.Usage.InputTokens != 200 || st.Usage.OutputTokens != 40 || st.Usage.CostUSD < 0.0019 || st.Usage.CostUSD > 0.0021 {
		t.Errorf("stats usage = %+v", *st.Usage)
	}
	out, _, _ = runKB(t, nil, "", "stats", "--scope", "hook")
	if !strings.Contains(out, "200 input") || !strings.Contains(out, "40 output") {
		t.Errorf("text stats should show usage totals:\n%s", out)
	}
}

func TestBuildCompilerFlagWinsOverEnv(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha"})
	env := []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=exit 7"}
	_, stderr, code := runKB(t, env, "", "build", src, "--scope", "fw", "--pattern", "*.md", "--compiler", fakeCompilerCommand(t, ""))
	if code != 0 {
		t.Fatalf("--compiler should win over KB_COMPILER: code=%d stderr=%s", code, stderr)
	}
}

func TestBuildCompilerFailureIsLoud(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha text", "b.md": "beta raw text MUST_NOT_BE_STORED"})
	env := []string{"KB_FAKE_COMPILER=fail-on:b.md", "KB_COMPILER=" + fakeCompilerCommand(t, "")}

	_, stderr, code := runKB(t, env, "", "build", src, "--scope", "loud", "--pattern", "*.md")
	if code == 0 {
		t.Fatalf("a failed item must make build exit non-zero; stderr=%s", stderr)
	}
	if !strings.Contains(stderr, "b.md") || !strings.Contains(stderr, "refusing") {
		t.Errorf("stderr should name the failed source and pass compiler stderr through: %s", stderr)
	}
	arts, _ := listArticles("loud")
	if len(arts) != 1 || arts[0].Title != "Fake a.md" {
		t.Fatalf("partial success must be saved and the failure must write nothing: %+v", arts)
	}
	for _, a := range arts {
		if strings.Contains(a.Content, "MUST_NOT_BE_STORED") {
			t.Errorf("raw text stored verbatim as an article")
		}
	}
	// The failed file is not cached: the next build retries it.
	env = []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=" + fakeCompilerCommand(t, "")}
	out, stderr, code := runKB(t, env, "", "build", src, "--scope", "loud", "--pattern", "*.md", "--json")
	if code != 0 || !strings.Contains(out, `"changed": 1`) {
		t.Errorf("retry should compile only b.md: code=%d out=%s stderr=%s", code, out, stderr)
	}
}

func TestIngestWithCompilerHook(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"notes.md": "meeting notes"})
	out, stderr, code := runKB(t, []string{"KB_FAKE_COMPILER=ok"}, "",
		"ingest", filepath.Join(src, "notes.md"), "--scope", "ing", "--json", "--compiler", fakeCompilerCommand(t, ""))
	if code != 0 {
		t.Fatalf("ingest with compiler failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(out, `"compiled_with": "fake-model"`) {
		t.Errorf("ingest --json should report compiled_with from usage.model: %s", out)
	}
}

func TestIngestCompilerFailureKeepsRawWritesNoArticle(t *testing.T) {
	isolatedHome(t)
	scope := "ing-fail"
	t.Setenv("KB_FAKE_COMPILER", "fail")
	spec := compilerSpec{Command: fakeCompilerCommand(t, ""), Timeout: 60 * time.Second, Stderr: io.Discard}
	text := "raw text that must not silently become an article"
	err := ingestText(scope, "notes.md", spec, "", "", text, false, false)
	if err == nil {
		t.Fatal("ingest must fail loudly when the compiler fails")
	}
	if !strings.Contains(err.Error(), contentHash(text)[:16]) || !strings.Contains(err.Error(), "--allow-fallback") {
		t.Errorf("error should name the raw doc and the escape hatch: %v", err)
	}
	if rawDocCount(t, scope) != 1 || wikiArticleCount(t, scope) != 0 {
		t.Errorf("raw doc kept, no article: raw=%d wiki=%d", rawDocCount(t, scope), wikiArticleCount(t, scope))
	}
}

func TestRecompileWithCompilerHook(t *testing.T) {
	isolatedHome(t)
	scope := "recomp"
	if err := ingestArticleJSON(scope, []byte(`{"raw_text":"original raw","article":{"title":"Orig","content":"orig body","source":"orig.md"}}`), false); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runKB(t, []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=" + fakeCompilerCommand(t, "")}, "",
		"recompile", "orig", "--scope", scope)
	if code != 0 {
		t.Fatalf("recompile failed: code=%d stderr=%s", code, stderr)
	}
	a, _ := loadArticle(scope, "orig")
	if a == nil || a.Version != 2 || a.Usage == nil || !strings.HasPrefix(a.Content, "Fake ") {
		t.Errorf("recompiled article = %+v", a)
	}

	_, stderr, code = runKB(t, []string{"KB_FAKE_COMPILER=fail", "KB_COMPILER=" + fakeCompilerCommand(t, "")}, "",
		"recompile", "orig", "--scope", scope)
	if code == 0 {
		t.Errorf("a failed recompile must exit non-zero; stderr=%s", stderr)
	}
	a2, _ := loadArticle(scope, "orig")
	if a2 == nil || a2.Version != 2 {
		t.Errorf("failed recompile must leave the article untouched: %+v", a2)
	}
}

func TestLintLLMWithCompilerHook(t *testing.T) {
	isolatedHome(t)
	scope := "lintllm"
	saveArticle(scope, &WikiArticle{ID: "a1", Title: "A1", Summary: "s", Content: "x", Concepts: []string{"c"}, Version: 1})
	out, stderr, code := runKB(t, []string{"KB_FAKE_COMPILER=lint", "KB_COMPILER=" + fakeCompilerCommand(t, "")}, "",
		"lint", "--llm", "--scope", scope, "--json")
	if code != 0 || !strings.Contains(out, "FAKE_LINT_ISSUE") {
		t.Errorf("lint --llm through the hook: code=%d out=%s stderr=%s", code, out, stderr)
	}
	_, stderr, code = runKB(t, []string{"KB_FAKE_COMPILER=garbage", "KB_COMPILER=" + fakeCompilerCommand(t, "")}, "",
		"lint", "--llm", "--scope", scope, "--json")
	if code == 0 {
		t.Errorf("unparseable lint output must fail loudly; stderr=%s", stderr)
	}
}

// --- usage metadata ---------------------------------------------------------

func TestArticleJSONStoresUsage(t *testing.T) {
	isolatedHome(t)
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

func TestAcceptStoresAndReplacesUsage(t *testing.T) {
	isolatedHome(t)
	scope := "acc-usage"
	first := `{"scope":"acc-usage","articles":[{"source":"a.md","hash":"h1","raw_id":"r1","title":"Acc","content":"body","usage":{"model":"m1","input_tokens":10,"output_tokens":2,"cost_usd":0.1}}]}`
	if _, stderr, code := runKB(t, nil, first, "accept", "--scope", scope); code != 0 {
		t.Fatalf("accept: %s", stderr)
	}
	a, _ := loadArticle(scope, "acc")
	if a == nil || a.Usage == nil || a.Usage.InputTokens != 10 || a.CompiledWith != "m1" {
		t.Fatalf("accept usage not stored: %+v", a)
	}
	second := strings.Replace(strings.Replace(first, `"input_tokens":10`, `"input_tokens":7`, 1), `"model":"m1"`, `"model":"m2"`, 1)
	runKB(t, nil, second, "accept", "--scope", scope)
	a, _ = loadArticle(scope, "acc")
	if a == nil || a.Usage == nil || a.Usage.InputTokens != 7 || a.Usage.Model != "m2" {
		t.Errorf("re-accept must replace usage, not sum: %+v", a.Usage)
	}
	// No usage at all: the old "agent" default and no usage block.
	third := `{"articles":[{"source":"b.md","raw_id":"r2","title":"NoUsage","content":"body"}]}`
	runKB(t, nil, third, "accept", "--scope", scope)
	b, _ := loadArticle(scope, "nousage")
	if b == nil || b.Usage != nil || b.CompiledWith != "agent" {
		t.Errorf("accept without usage: %+v", b)
	}
}

func TestUsageFrontmatterBackwardCompatible(t *testing.T) {
	isolatedHome(t)
	scope := "fm-usage"
	saveArticle(scope, &WikiArticle{ID: "legacy", Title: "Legacy", Content: "x", Version: 1})
	raw, _ := os.ReadFile(filepath.Join(scopeDir(scope), "wiki", "legacy.md"))
	if strings.Contains(string(raw), `"usage"`) {
		t.Errorf("articles without usage must not grow a usage key:\n%s", raw)
	}
	a, _ := loadArticle(scope, "legacy")
	if a.Usage != nil {
		t.Errorf("legacy article should load with nil usage")
	}
	n, _, _, _ := usageTotals([]*WikiArticle{a})
	if n != 0 {
		t.Errorf("no usage → zero articles counted")
	}
	out, _, _ := runKB(t, nil, "", "stats", "--scope", scope, "--json")
	if strings.Contains(out, `"usage"`) {
		t.Errorf("stats --json must omit usage when no article has it:\n%s", out)
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

func TestVersionCommandIsNotHardcoded(t *testing.T) {
	out, _, code := runKB(t, nil, "", "version")
	if code != 0 || strings.Contains(out, "v0.1.0") || !strings.HasPrefix(out, "kb ") {
		t.Errorf("kb version = %q", out)
	}
}
