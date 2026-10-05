// Tests for the built-in Anthropic client and the compile-path precedence
// (--compiler > KB_COMPILER > built-in when ANTHROPIC_API_KEY is set > exit 2).
//
// No test talks to the real API: each points ANTHROPIC_BASE_URL (or
// compilerSpec.BaseURL) at an httptest server that speaks just enough of the
// Messages API: it echoes the requested model, returns a fixed article built
// from the prompt's "Source:" line, and reports fixed token usage.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const (
	stubInputTokens  = 123
	stubOutputTokens = 45
)

// stubAnthropic records every request it receives.
type stubAnthropic struct {
	*httptest.Server
	mu      sync.Mutex
	paths   []string
	keys    []string
	version []string
	models  []string
}

func (s *stubAnthropic) hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.paths)
}

// newStubAnthropic starts a fake Messages endpoint. status != 200 makes every
// request fail with that status; reply, when non-nil, overrides the text the
// model "says" (given the prompt).
func newStubAnthropic(t *testing.T, status int, reply func(prompt string) string) *stubAnthropic {
	t.Helper()
	s := &stubAnthropic{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(body, &req)
		s.mu.Lock()
		s.paths = append(s.paths, r.Method+" "+r.URL.Path)
		s.keys = append(s.keys, r.Header.Get("x-api-key"))
		s.version = append(s.version, r.Header.Get("anthropic-version"))
		s.models = append(s.models, req.Model)
		s.mu.Unlock()

		if status != http.StatusOK {
			http.Error(w, `{"type":"error","error":{"type":"overloaded_error","message":"stub overloaded"}}`, status)
			return
		}
		prompt := ""
		if len(req.Messages) > 0 {
			prompt = req.Messages[0].Content
		}
		var text string
		if reply != nil {
			text = reply(prompt)
		} else {
			source := "unknown"
			for _, line := range strings.Split(prompt, "\n") {
				if strings.HasPrefix(line, "Source: ") {
					source = strings.TrimSpace(strings.TrimPrefix(line, "Source: "))
					break
				}
			}
			b, _ := json.Marshal(map[string]any{
				"title":      "Builtin " + source,
				"summary":    "Compiled by the stub Messages API.",
				"content":    "Stub article for " + source + ".",
				"concepts":   []string{"stub"},
				"categories": []string{"Testing"},
			})
			text = string(b)
		}
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"model":   req.Model,
			"content": []map[string]string{{"type": "text", "text": text}},
			"usage":   map[string]int{"input_tokens": stubInputTokens, "output_tokens": stubOutputTokens},
		})
	}))
	t.Cleanup(s.Close)
	return s
}

// builtinEnv is the env for exec'd kb using the built-in client against s.
func builtinEnv(s *stubAnthropic) []string {
	return []string{"KB_COMPILER=", "ANTHROPIC_API_KEY=sk-dummy", "ANTHROPIC_BASE_URL=" + s.URL}
}

func TestMessagesURL(t *testing.T) {
	cases := map[string]string{
		"":                           "https://api.anthropic.com/v1/messages",
		"https://api.anthropic.com":  "https://api.anthropic.com/v1/messages",
		"http://localhost:4000/":     "http://localhost:4000/v1/messages",
		"https://gw.example/litellm": "https://gw.example/litellm/v1/messages",
	}
	for base, want := range cases {
		if got := messagesURL(base); got != want {
			t.Errorf("messagesURL(%q) = %q, want %q", base, got, want)
		}
	}
	t.Setenv("ANTHROPIC_BASE_URL", "")
	if got := anthropicBaseURL(); got != defaultBaseURL {
		t.Errorf("default base = %q", got)
	}
	t.Setenv("ANTHROPIC_BASE_URL", " http://proxy:4000 ")
	if got := anthropicBaseURL(); got != "http://proxy:4000" {
		t.Errorf("ANTHROPIC_BASE_URL not honoured: %q", got)
	}
}

func TestBuiltinCompileHappyPath(t *testing.T) {
	s := newStubAnthropic(t, http.StatusOK, nil)
	spec := compilerSpec{APIKey: "sk-dummy", Model: "claude-test", BaseURL: s.URL + "/"}
	art, err := compileArticle(spec, "package main\nfunc main() {}\n", "cmd/app/main.go", nil, true)
	if err != nil {
		t.Fatalf("compileArticle: %v", err)
	}
	if art.Title != "Builtin cmd/app/main.go" || art.Audience != "agent" || art.TargetWords != 150 {
		t.Errorf("article = %+v", art)
	}
	if art.CompiledWith != "claude-test" {
		t.Errorf("CompiledWith = %q, want the model name", art.CompiledWith)
	}
	if art.Usage == nil || art.Usage.Model != "claude-test" || art.Usage.InputTokens != stubInputTokens ||
		art.Usage.OutputTokens != stubOutputTokens || art.Usage.CostUSD != 0 {
		t.Errorf("usage = %+v (want model+tokens, no invented cost)", art.Usage)
	}
	if s.paths[0] != "POST /v1/messages" || s.keys[0] != "sk-dummy" || s.version[0] != apiVersion || s.models[0] != "claude-test" {
		t.Errorf("request = %v %v %v %v", s.paths, s.keys, s.version, s.models)
	}
}

func TestBuiltinNon200IsLoud(t *testing.T) {
	s := newStubAnthropic(t, 529, nil)
	spec := compilerSpec{APIKey: "sk-dummy", Model: defaultModel, BaseURL: s.URL}
	art, err := compileArticle(spec, "text", "doc.md", nil, false)
	if err == nil || art != nil || !strings.Contains(err.Error(), "API error 529") {
		t.Fatalf("non-200 must be an error with no article: art=%v err=%v", art, err)
	}

	// ingest keeps the v0.3.0 loud-fail contract: raw doc kept, no article.
	isolatedHome(t)
	text := "raw text that must not silently become an article"
	err = ingestText("bi-fail", "notes.md", spec, "", "", text, false, false)
	if err == nil || !strings.Contains(err.Error(), contentHash(text)[:16]) || !strings.Contains(err.Error(), "--allow-fallback") {
		t.Fatalf("ingest must fail loudly naming the raw doc and --allow-fallback: %v", err)
	}
	if rawDocCount(t, "bi-fail") != 1 || wikiArticleCount(t, "bi-fail") != 0 {
		t.Errorf("raw doc kept, no article: raw=%d wiki=%d", rawDocCount(t, "bi-fail"), wikiArticleCount(t, "bi-fail"))
	}
	// --allow-fallback stores it verbatim, as in v0.3.0.
	if err := ingestText("bi-fail", "notes.md", spec, "", "", text, true, false); err != nil {
		t.Fatalf("--allow-fallback: %v", err)
	}
	arts, _ := listArticles("bi-fail")
	if len(arts) != 1 || arts[0].CompiledWith != "none (fallback)" {
		t.Errorf("fallback article = %+v", arts)
	}

	// build: exit 1, no article, nothing cached.
	src := writeFiles(t, map[string]string{"a.md": "alpha RAW_MUST_NOT_BE_STORED"})
	_, stderr, code := runKB(t, builtinEnv(s), "", "build", src, "--scope", "bi-build-fail", "--pattern", "*.md")
	if code != 1 || !strings.Contains(stderr, "API error 529") {
		t.Errorf("build with a failing API: code=%d stderr=%s", code, stderr)
	}
	if n := wikiArticleCount(t, "bi-build-fail"); n != 0 {
		t.Errorf("a failed compile must not store an article, got %d", n)
	}
}

func TestBuildWithBuiltinClient(t *testing.T) {
	isolatedHome(t)
	s := newStubAnthropic(t, http.StatusOK, nil)
	src := writeFiles(t, map[string]string{"a.md": "# A\nalpha", "b.md": "# B\nbeta"})

	out, stderr, code := runKB(t, builtinEnv(s), "", "build", src, "--scope", "bi", "--pattern", "*.md", "--json")
	if code != 0 {
		t.Fatalf("build failed: code=%d stderr=%s", code, stderr)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("build --json: %v\n%s", err, out)
	}
	if res["changed"].(float64) != 2 || res["input_tokens"].(float64) != 2*stubInputTokens || res["output_tokens"].(float64) != 2*stubOutputTokens {
		t.Errorf("build json = %v", res)
	}
	if s.hits() != 2 || s.models[0] != defaultModel {
		t.Errorf("stub saw %d requests, models %v", s.hits(), s.models)
	}
	a, _ := loadArticle("bi", slugify("Builtin a.md"))
	if a == nil || a.CompiledWith != defaultModel || a.Usage == nil || a.Usage.InputTokens != stubInputTokens {
		t.Fatalf("article = %+v", a)
	}

	out, _, _ = runKB(t, nil, "", "stats", "--scope", "bi")
	if !strings.Contains(out, fmt.Sprintf("Compile usage (2 articles): %d input + %d output tokens", 2*stubInputTokens, 2*stubOutputTokens)) {
		t.Errorf("stats should total built-in usage:\n%s", out)
	}
	if strings.Contains(out, "$") {
		t.Errorf("no cost may be reported for the built-in path:\n%s", out)
	}

	// --model reaches the request and compiled_with (recompile path).
	_, stderr, code = runKB(t, builtinEnv(s), "", "recompile", "--all", "--scope", "bi", "--model", "claude-other")
	if code != 0 {
		t.Fatalf("recompile: code=%d stderr=%s", code, stderr)
	}
	a, _ = loadArticle("bi", slugify("Builtin a.md"))
	if a == nil || a.CompiledWith != "claude-other" || a.Version != 2 || s.models[len(s.models)-1] != "claude-other" {
		t.Errorf("--model not applied: article=%+v models=%v", a, s.models)
	}
}

func TestIngestAndLintWithBuiltinClient(t *testing.T) {
	isolatedHome(t)
	s := newStubAnthropic(t, http.StatusOK, nil)
	out, stderr, code := runKB(t, builtinEnv(s), "meeting notes", "ingest", "--scope", "bi-ing", "--source", "notes.md", "--json")
	if code != 0 || !strings.Contains(out, `"compiled_with": "`+defaultModel+`"`) {
		t.Fatalf("ingest via built-in: code=%d out=%s stderr=%s", code, out, stderr)
	}

	lint := newStubAnthropic(t, http.StatusOK, func(string) string {
		return `[{"type":"gap","severity":"warning","message":"STUB_LINT_ISSUE"}]`
	})
	out, stderr, code = runKB(t, builtinEnv(lint), "", "lint", "--llm", "--scope", "bi-ing", "--json")
	if code != 0 || !strings.Contains(out, "STUB_LINT_ISSUE") {
		t.Errorf("lint --llm via built-in: code=%d out=%s stderr=%s", code, out, stderr)
	}
}

// Precedence: --compiler > KB_COMPILER > built-in (key set). A key in the
// environment never overrides a configured compiler.
func TestCompilePathPrecedence(t *testing.T) {
	isolatedHome(t)
	src := writeFiles(t, map[string]string{"a.md": "alpha"})
	cases := []struct {
		name      string
		env       []string
		args      []string
		wantStub  bool
		wantModel string
	}{
		{"key only: built-in", nil, nil, true, defaultModel},
		{"KB_COMPILER beats key", []string{"KB_COMPILER=" + fakeCompilerCommand(t, "")}, nil, false, "fake-model"},
		{"--compiler beats key", nil, []string{"--compiler", fakeCompilerCommand(t, "")}, false, "fake-model"},
		{"--compiler beats KB_COMPILER and key", []string{"KB_COMPILER=exit 7"}, []string{"--compiler", fakeCompilerCommand(t, "")}, false, "fake-model"},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := newStubAnthropic(t, http.StatusOK, nil)
			scope := fmt.Sprintf("prec%d", i)
			env := append(builtinEnv(s), "KB_FAKE_COMPILER=ok")
			env = append(env, c.env...)
			args := append([]string{"build", src, "--scope", scope, "--pattern", "*.md"}, c.args...)
			_, stderr, code := runKB(t, env, "", args...)
			if code != 0 {
				t.Fatalf("build: code=%d stderr=%s", code, stderr)
			}
			if got := s.hits() > 0; got != c.wantStub {
				t.Errorf("built-in client used = %v, want %v", got, c.wantStub)
			}
			arts, _ := listArticles(scope)
			if len(arts) != 1 || arts[0].CompiledWith != c.wantModel {
				t.Errorf("articles = %+v, want compiled_with %q", arts, c.wantModel)
			}
		})
	}
}
