// In-process tests for the built-in Anthropic client: the endpoint URL, a
// compile against the Messages API, and the loud failure on a non-200 reply
// (compile, ingest and build). Binary-level tests of the built-in path and of
// the compile-path precedence live in e2e_test.go.
//
// No test talks to the real API: each points ANTHROPIC_BASE_URL (or
// compilerSpec.BaseURL) at kbtest.NewStubAnthropic, an httptest server that
// speaks just enough of the Messages API: it echoes the requested model,
// returns a fixed article built from the prompt's "Source:" line, and reports
// fixed token usage.
package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/textutil"
)

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
	s := kbtest.NewStubAnthropic(t, http.StatusOK, nil)
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
	if art.Usage == nil || art.Usage.Model != "claude-test" || art.Usage.InputTokens != kbtest.StubInputTokens ||
		art.Usage.OutputTokens != kbtest.StubOutputTokens || art.Usage.CostUSD != 0 {
		t.Errorf("usage = %+v (want model+tokens, no invented cost)", art.Usage)
	}
	if s.Paths[0] != "POST /v1/messages" || s.Keys[0] != "sk-dummy" || s.Versions[0] != apiVersion || s.Models[0] != "claude-test" {
		t.Errorf("request = %v %v %v %v", s.Paths, s.Keys, s.Versions, s.Models)
	}
}

func TestBuiltinNon200IsLoud(t *testing.T) {
	s := kbtest.NewStubAnthropic(t, 529, nil)
	spec := compilerSpec{APIKey: "sk-dummy", Model: defaultModel, BaseURL: s.URL}
	art, err := compileArticle(spec, "text", "doc.md", nil, false)
	if err == nil || art != nil || !strings.Contains(err.Error(), "API error 529") {
		t.Fatalf("non-200 must be an error with no article: art=%v err=%v", art, err)
	}

	// ingest keeps the v0.3.0 loud-fail contract: raw doc kept, no article.
	kbtest.IsolatedHome(t)
	text := "raw text that must not silently become an article"
	err = ingestText("bi-fail", "notes.md", spec, "", "", text, false, false)
	if err == nil || !strings.Contains(err.Error(), textutil.ContentHash(text)[:16]) || !strings.Contains(err.Error(), "--allow-fallback") {
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
	_, stderr, code := kbtest.RunKB(t, s.Env(), "", "build", src, "--scope", "bi-build-fail", "--pattern", "*.md")
	if code != 1 || !strings.Contains(stderr, "API error 529") {
		t.Errorf("build with a failing API: code=%d stderr=%s", code, stderr)
	}
	if n := wikiArticleCount(t, "bi-build-fail"); n != 0 {
		t.Errorf("a failed compile must not store an article, got %d", n)
	}
}
