// In-process tests for the built-in Anthropic client: the endpoint URL
// (messagesURL, BaseURLFromEnv) and a compile against the Messages API with
// the request, compiled_with and usage it produces. No test talks to the real
// API: each points Spec.BaseURL (or ANTHROPIC_BASE_URL) at
// kbtest.NewStubAnthropic, a local fake Messages API.

package compile

import (
	"net/http"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
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
	if got := BaseURLFromEnv(); got != defaultBaseURL {
		t.Errorf("default base = %q", got)
	}
	t.Setenv("ANTHROPIC_BASE_URL", " http://proxy:4000 ")
	if got := BaseURLFromEnv(); got != "http://proxy:4000" {
		t.Errorf("ANTHROPIC_BASE_URL not honoured: %q", got)
	}
}

func TestBuiltinCompileHappyPath(t *testing.T) {
	s := kbtest.NewStubAnthropic(t, http.StatusOK, nil)
	spec := Spec{APIKey: "sk-dummy", Model: "claude-test", BaseURL: s.URL + "/"}
	art, err := Article(spec, "package main\nfunc main() {}\n", "cmd/app/main.go", "", true)
	if err != nil {
		t.Fatalf("Article: %v", err)
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
