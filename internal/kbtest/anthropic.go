// A stub Anthropic Messages API for the built-in client's tests, so no test
// ever needs a real key or network. NewStubAnthropic starts an httptest server
// that records every request, echoes the requested model, answers with a
// fixed article built from the prompt's "Source:" line (or a caller-supplied
// reply), and reports fixed token usage.

package kbtest

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Token usage the stub reports on every successful reply.
const (
	StubInputTokens  = 123
	StubOutputTokens = 45
)

// StubAnthropic is a running fake Messages endpoint. The recorded request
// fields are safe to read once the requests have completed.
type StubAnthropic struct {
	*httptest.Server
	mu       sync.Mutex
	Paths    []string // "METHOD /path"
	Keys     []string // x-api-key
	Versions []string // anthropic-version
	Models   []string // request body "model"
}

// Hits is the number of requests received so far.
func (s *StubAnthropic) Hits() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.Paths)
}

// Env is the env for an exec'd kb that should use the built-in client
// against this stub (and no compiler hook).
func (s *StubAnthropic) Env() []string {
	return []string{"KB_COMPILER=", "ANTHROPIC_API_KEY=sk-dummy", "ANTHROPIC_BASE_URL=" + s.URL}
}

// NewStubAnthropic starts a fake Messages endpoint, closed at test cleanup.
// status != 200 makes every request fail with that status; reply, when
// non-nil, overrides the text the model "says" (given the prompt).
func NewStubAnthropic(t testing.TB, status int, reply func(prompt string) string) *StubAnthropic {
	t.Helper()
	s := &StubAnthropic{}
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
		s.Paths = append(s.Paths, r.Method+" "+r.URL.Path)
		s.Keys = append(s.Keys, r.Header.Get("x-api-key"))
		s.Versions = append(s.Versions, r.Header.Get("anthropic-version"))
		s.Models = append(s.Models, req.Model)
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
			"usage":   map[string]int{"input_tokens": StubInputTokens, "output_tokens": StubOutputTokens},
		})
	}))
	t.Cleanup(s.Close)
	return s
}
