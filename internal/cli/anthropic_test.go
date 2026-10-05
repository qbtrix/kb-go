// The built-in Anthropic client's loud failure on a non-200 reply, through
// compile.Article, ingest and build. The client's own unit tests live in
// internal/compile; binary-level tests of the built-in path and of the
// compile-path precedence live in the root e2e_test.go.
//
// No test talks to the real API: each points ANTHROPIC_BASE_URL (or
// compile.Spec.BaseURL) at kbtest.NewStubAnthropic, a local fake Messages API.
package cli

import (
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func TestBuiltinNon200IsLoud(t *testing.T) {
	s := kbtest.NewStubAnthropic(t, 529, nil)
	spec := compile.Spec{APIKey: "sk-dummy", Model: compile.DefaultModel, BaseURL: s.URL}
	art, err := compile.Article(spec, "text", "doc.md", "", false)
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
	arts, _ := store.ListArticles("bi-fail")
	if len(arts) != 1 || arts[0].CompiledWith != "none (fallback)" {
		t.Errorf("fallback article = %+v", arts)
	}

	// build: exit 1, no article, nothing cached.
	src := kbtest.WriteFiles(t, map[string]string{"a.md": "alpha RAW_MUST_NOT_BE_STORED"})
	_, stderr, code := kbtest.RunKB(t, s.Env(), "", "build", src, "--scope", "bi-build-fail", "--pattern", "*.md")
	if code != 1 || !strings.Contains(stderr, "API error 529") {
		t.Errorf("build with a failing API: code=%d stderr=%s", code, stderr)
	}
	if n := wikiArticleCount(t, "bi-build-fail"); n != 0 {
		t.Errorf("a failed compile must not store an article, got %d", n)
	}
}
