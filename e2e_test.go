// End-to-end tests: each one execs the real kb binary (kbtest.BuildBinary
// builds the module root once per run) in an isolated home and checks exit
// codes, stdout/stderr and what lands on disk. Covers the --compiler hook
// through every command that uses it (build, ingest, recompile, lint --llm),
// the exit-2 refusals without a compiler, the removed --model flag, accept
// usage metadata, `kb version`, the glossary-only build, lint
// --normalize-categories, and MCP-vs-CLI search parity and latency.
//
// The compiler under test is the test binary itself re-executed: kbtest.Main
// (this package's TestMain) acts as a compiler when KB_FAKE_COMPILER is set, so
// the command string goes through the real platform shell (sh -c /
// cmd /S /C) exactly as a user's would.

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

// runKB executes the built kb binary with extra env and optional stdin.
func runKB(t *testing.T, env []string, stdin string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	// A hard deadline turns a command that wrongly blocks (e.g. watch without a
	// compiler) into a test failure instead of a hung suite.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, kbtest.BuildBinary(t), args...)
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

func TestCommandsWithoutCompilerExit2(t *testing.T) {
	kbtest.IsolatedHome(t)
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
	kbtest.IsolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha"})
	_, stderr, code := runKB(t, nil, "", "build", src, "--scope", "m", "--pattern", "*.md", "--model", "claude-haiku")
	if code != 2 || !strings.Contains(stderr, "--model") || !strings.Contains(stderr, "--compiler") {
		t.Errorf("--model should be rejected with exit 2 and point at --compiler; code=%d stderr=%s", code, stderr)
	}
}

func TestBuildGlossaryOnlyNeedsNoCompiler(t *testing.T) {
	kbtest.IsolatedHome(t)
	src := writeFiles(t, map[string]string{
		"glossary/pocket.md": "---\n{\"id\":\"pocket\",\"title\":\"Pocket\",\"kind\":\"glossary\",\"term\":\"Pocket\"}\n---\n\nA Pocket is a workspace.",
	})
	_, stderr, code := runKB(t, []string{"KB_COMPILER="}, "", "build", src, "--scope", "gl", "--pattern", "*.md")
	if code != 0 {
		t.Fatalf("glossary-only build needs no compiler; code=%d stderr=%s", code, stderr)
	}
}

func TestBuildWithCompilerHook(t *testing.T) {
	kbtest.IsolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "# A\nalpha", "b.md": "# B\nbeta"})
	env := []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}

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
	kbtest.IsolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha"})
	env := []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=exit 7"}
	_, stderr, code := runKB(t, env, "", "build", src, "--scope", "fw", "--pattern", "*.md", "--compiler", kbtest.FakeCompilerCommand(t, ""))
	if code != 0 {
		t.Fatalf("--compiler should win over KB_COMPILER: code=%d stderr=%s", code, stderr)
	}
}

func TestBuildCompilerFailureIsLoud(t *testing.T) {
	kbtest.IsolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha text", "b.md": "beta raw text MUST_NOT_BE_STORED"})
	env := []string{"KB_FAKE_COMPILER=fail-on:b.md", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}

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
	env = []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}
	out, stderr, code := runKB(t, env, "", "build", src, "--scope", "loud", "--pattern", "*.md", "--json")
	if code != 0 || !strings.Contains(out, `"changed": 1`) {
		t.Errorf("retry should compile only b.md: code=%d out=%s stderr=%s", code, out, stderr)
	}
}

func TestIngestWithCompilerHook(t *testing.T) {
	kbtest.IsolatedHome(t)
	src := writeFiles(t, map[string]string{"notes.md": "meeting notes"})
	out, stderr, code := runKB(t, []string{"KB_FAKE_COMPILER=ok"}, "",
		"ingest", filepath.Join(src, "notes.md"), "--scope", "ing", "--json", "--compiler", kbtest.FakeCompilerCommand(t, ""))
	if code != 0 {
		t.Fatalf("ingest with compiler failed: code=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(out, `"compiled_with": "fake-model"`) {
		t.Errorf("ingest --json should report compiled_with from usage.model: %s", out)
	}
}

func TestRecompileWithCompilerHook(t *testing.T) {
	kbtest.IsolatedHome(t)
	scope := "recomp"
	if err := ingestArticleJSON(scope, []byte(`{"raw_text":"original raw","article":{"title":"Orig","content":"orig body","source":"orig.md"}}`), false); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := runKB(t, []string{"KB_FAKE_COMPILER=ok", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}, "",
		"recompile", "orig", "--scope", scope)
	if code != 0 {
		t.Fatalf("recompile failed: code=%d stderr=%s", code, stderr)
	}
	a, _ := loadArticle(scope, "orig")
	if a == nil || a.Version != 2 || a.Usage == nil || !strings.HasPrefix(a.Content, "Fake ") {
		t.Errorf("recompiled article = %+v", a)
	}

	_, stderr, code = runKB(t, []string{"KB_FAKE_COMPILER=fail", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}, "",
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
	kbtest.IsolatedHome(t)
	scope := "lintllm"
	saveArticle(scope, &WikiArticle{ID: "a1", Title: "A1", Summary: "s", Content: "x", Concepts: []string{"c"}, Version: 1})
	out, stderr, code := runKB(t, []string{"KB_FAKE_COMPILER=lint", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}, "",
		"lint", "--llm", "--scope", scope, "--json")
	if code != 0 || !strings.Contains(out, "FAKE_LINT_ISSUE") {
		t.Errorf("lint --llm through the hook: code=%d out=%s stderr=%s", code, out, stderr)
	}
	_, stderr, code = runKB(t, []string{"KB_FAKE_COMPILER=garbage", "KB_COMPILER=" + kbtest.FakeCompilerCommand(t, "")}, "",
		"lint", "--llm", "--scope", scope, "--json")
	if code == 0 {
		t.Errorf("unparseable lint output must fail loudly; stderr=%s", stderr)
	}
}

func TestAcceptStoresAndReplacesUsage(t *testing.T) {
	kbtest.IsolatedHome(t)
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
	kbtest.IsolatedHome(t)
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

func TestVersionCommandIsNotHardcoded(t *testing.T) {
	out, _, code := runKB(t, nil, "", "version")
	if code != 0 || strings.Contains(out, "v0.1.0") || !strings.HasPrefix(out, "kb ") {
		t.Errorf("kb version = %q", out)
	}
}

func TestNormalizeCategoriesCLIEmptyScope(t *testing.T) {
	// Integration test: exec the binary against an empty scope and confirm
	// the no-op path behaves correctly (prints the "no articles" message,
	// exits 0). Exercises runCategoryNormalize end-to-end via the CLI.
	scope := "test-cli-empty-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()
	// Intentionally no articles — scope is empty.
	_ = os.MkdirAll(scopeDir(scope), 0o755)

	binary := kbtest.BuildBinary(t)
	out, err := exec.Command(binary, "lint", "--scope", scope, "--normalize-categories").CombinedOutput()
	if err != nil {
		t.Fatalf("kb lint --normalize-categories failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "No articles in scope") {
		t.Errorf("expected 'No articles in scope' message, got:\n%s", out)
	}
}

func TestNormalizeCategoriesCLIJSONMode(t *testing.T) {
	// Integration test: --json output on a scope with a real cluster.
	scope := "test-cli-json-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	articles := []*WikiArticle{
		{ID: "a1", Title: "A1", Content: "x", Categories: []string{"CLI"}, Version: 1},
		{ID: "a2", Title: "A2", Content: "x", Categories: []string{"cli"}, Version: 1},
	}
	for _, a := range articles {
		if err := saveArticle(scope, a); err != nil {
			t.Fatalf("save: %v", err)
		}
	}

	binary := kbtest.BuildBinary(t)
	out, err := exec.Command(binary, "lint", "--scope", scope, "--normalize-categories", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("kb lint --normalize-categories --json failed: %v\n%s", err, out)
	}

	var payload struct {
		Scope    string            `json:"scope"`
		Applied  bool              `json:"applied"`
		Clusters []categoryCluster `json:"clusters"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("json parse failed: %v\n%s", err, out)
	}
	if payload.Scope != scope {
		t.Errorf("scope = %q, want %q", payload.Scope, scope)
	}
	if payload.Applied {
		t.Error("applied should be false for dry run")
	}
	if len(payload.Clusters) != 1 {
		t.Fatalf("expected 1 cluster, got %d", len(payload.Clusters))
	}
	if payload.Clusters[0].Canonical != "cli" {
		t.Errorf("canonical = %q, want 'cli'", payload.Clusters[0].Canonical)
	}
}

// normalizeSearchJSON parses a search JSON payload and re-marshals it in a
// canonical key order so the two sources can be compared regardless of map
// iteration order.
func normalizeSearchJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("decode search json %q: %v", raw, err)
	}
	canon, err := json.Marshal(arr)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(canon)
}

func TestMCPSearchParityWithCLI(t *testing.T) {
	scope := seedSampleKB(t)
	binary := kbtest.BuildBinary(t)
	const query = "middleware authentication"

	// CLI path: spawn the process, parse stdout JSON.
	cliOut, err := exec.Command(binary, "search", query, "--scope", scope, "--json", "--limit", "5").Output()
	if err != nil {
		t.Fatalf("cli search: %v", err)
	}
	cliCanon := normalizeSearchJSON(t, cliOut)

	// MCP path: drive the server over its real stdio transport (subprocess),
	// so this exercises the same code the agent host would.
	cmd := exec.Command(binary, "serve", "--scope", scope)
	stdin, _ := cmd.StdinPipe()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	callParams, _ := json.Marshal(map[string]any{
		"name":      "kb_search",
		"arguments": map[string]any{"query": query, "scope": scope, "limit": 5},
	})
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":%s}`+"\n", callParams)
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("serve wait: %v\n%s", err, stdout.String())
	}

	// Pull the tools/call (id:2) response and extract its text content.
	var mcpCanon string
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var r rpcResponse
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		var id int
		json.Unmarshal(r.ID, &id)
		if id != 2 {
			continue
		}
		text := toolText(t, "kb_search", r.Result)
		mcpCanon = normalizeSearchJSON(t, []byte(text))
	}
	if mcpCanon == "" {
		t.Fatalf("no kb_search response found in serve output:\n%s", stdout.String())
	}

	if cliCanon != mcpCanon {
		t.Fatalf("parity mismatch:\n CLI: %s\n MCP: %s", cliCanon, mcpCanon)
	}
	t.Logf("parity OK: CLI and MCP return identical search JSON (%d bytes)", len(cliCanon))
}

func TestMCPLatencyDelta(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency measurement in -short mode")
	}
	scope := seedSampleKB(t)
	binary := kbtest.BuildBinary(t)
	const n = 20
	queries := []string{
		"middleware authentication", "rate limiting", "bm25 search index",
		"tokens", "gateway throttle",
	}

	// (1) Cold CLI: spawn a fresh process for each query.
	cliStart := time.Now()
	for i := 0; i < n; i++ {
		q := queries[i%len(queries)]
		if _, err := exec.Command(binary, "search", q, "--scope", scope, "--json").Output(); err != nil {
			t.Fatalf("cli query %d: %v", i, err)
		}
	}
	cliTotal := time.Since(cliStart)

	// (2) Persistent server: one process, N tools/call over the same stdio.
	cmd := exec.Command(binary, "serve", "--scope", scope)
	stdin, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	reader := bufio.NewReader(stdoutPipe)
	readLine := func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read serve response: %v", err)
		}
		_ = line
	}
	// Handshake (not counted).
	fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`)
	readLine()

	srvStart := time.Now()
	for i := 0; i < n; i++ {
		q := queries[i%len(queries)]
		callParams, _ := json.Marshal(map[string]any{
			"name":      "kb_search",
			"arguments": map[string]any{"query": q, "scope": scope},
		})
		fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`+"\n", i+1, callParams)
		readLine()
	}
	srvTotal := time.Since(srvStart)
	stdin.Close()
	cmd.Wait()

	cliPer := cliTotal / n
	srvPer := srvTotal / n
	speedup := float64(cliPer) / float64(srvPer)

	t.Logf("latency over %d queries (sample KB, scope=%s):", n, scope)
	t.Logf("  cold CLI  (spawn/query): %v total, %v per query", cliTotal.Round(time.Microsecond), cliPer.Round(time.Microsecond))
	t.Logf("  persistent server      : %v total, %v per query", srvTotal.Round(time.Microsecond), srvPer.Round(time.Microsecond))
	t.Logf("  speedup                : %.1fx (per-query)", speedup)

	if srvPer >= cliPer {
		t.Errorf("expected persistent server to beat cold CLI per-query, got srv=%v cli=%v", srvPer, cliPer)
	}
}

// TODO: passes after glossary feature lands
func TestGlossaryBuildPreservesBodyVerbatim(t *testing.T) {
	// Use the binary subprocess pattern (matches TestNormalizeCategoriesCLI* in
	// kb_test.go). With no compiler configured, a build that needed one would
	// refuse (exit 2) — so this passes only because cmdBuild's glossary branch
	// parses the frontmatter and populates Kind/Term/Aliases/Category/Related
	// from the source file without compiling.
	srcRoot := t.TempDir()
	glossaryDir := filepath.Join(srcRoot, "glossary")
	if err := os.MkdirAll(glossaryDir, 0o755); err != nil {
		t.Fatalf("mkdir glossary: %v", err)
	}

	const marker = "VERBATIM_BODY_MARKER_42"
	src := `---
{
  "id": "pocket",
  "title": "Pocket",
  "kind": "glossary",
  "term": "Pocket",
  "aliases": ["pkt", "pocket"],
  "category": "workspace-primitives",
  "related": ["Soul", "Fabric"],
  "concepts": [],
  "categories": [],
  "word_count": 12
}
---

A Pocket is a workspace container. ` + marker + ` lives in this body.`

	if err := os.WriteFile(filepath.Join(glossaryDir, "pocket.md"), []byte(src), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}

	scope := "test-gloss-build-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	binary := kbtest.BuildBinary(t)
	cmd := exec.Command(binary, "build", srcRoot, "--scope", scope, "--pattern", "*.md")
	// Deliberately clear KB_COMPILER: the glossary branch must bypass the
	// compiler entirely.
	cmd.Env = append(os.Environ(), "KB_COMPILER=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("kb build failed: %v\noutput: %s", err, out)
	}

	articles, err := listArticles(scope)
	if err != nil {
		t.Fatalf("listArticles: %v", err)
	}
	if len(articles) == 0 {
		t.Fatalf("no articles produced by build; stdout: %s", out)
	}

	var glossaryArt *WikiArticle
	for _, a := range articles {
		if a.Kind == "glossary" {
			glossaryArt = a
			break
		}
	}
	if glossaryArt == nil {
		t.Fatalf("no article with Kind=\"glossary\" produced. articles: %+v", articles)
	}
	if glossaryArt.Term != "Pocket" {
		t.Errorf("Term = %q, want %q", glossaryArt.Term, "Pocket")
	}
	if !strings.Contains(glossaryArt.Content, marker) {
		t.Errorf("Content missing verbatim marker %q. Got: %q", marker, glossaryArt.Content)
	}
}
