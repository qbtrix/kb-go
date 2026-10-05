// accept_collision_test.go pins article identity across the externally-compiled
// write paths (accept, ingest --article-json): an article's id must not collide
// with a different source's article just because the LLM gave both the same
// title, and re-compiling one source under a new title must replace its article
// rather than leave a stale twin behind.
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// runAccept feeds payload to cmdAccept on stdin and swallows its stdout.
func runAccept(t *testing.T, scope string, payload any) {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	origIn, origOut := os.Stdin, os.Stdout
	ir, iw, _ := os.Pipe()
	or, ow, _ := os.Pipe()
	os.Stdin, os.Stdout = ir, ow
	go func() { iw.Write(data); iw.Close() }()
	cmdAccept([]string{"--scope", scope, "--json"})
	ow.Close()
	os.Stdin, os.Stdout = origIn, origOut
	var sink bytes.Buffer
	sink.ReadFrom(or)
}

func sourcePaths(t *testing.T, scope string) []string {
	t.Helper()
	arts, err := store.ListArticles(scope)
	if err != nil {
		t.Fatalf("listArticles: %v", err)
	}
	var out []string
	for _, a := range arts {
		out = append(out, a.SourcePath)
	}
	sort.Strings(out)
	return out
}

func acceptItem(source, title string) map[string]any {
	return map[string]any{
		"source": source, "hash": textutil.ContentHash(source + title), "raw_id": textutil.ContentHash(source)[:16],
		"title": title, "summary": "s", "content": "content of " + source,
	}
}

func TestAcceptSameTitleDifferentSourcesKeepsBoth(t *testing.T) {
	scope := "test-accept-collide-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	runAccept(t, scope, map[string]any{"articles": []any{
		acceptItem("api/README.md", "Overview"),
		acceptItem("web/README.md", "Overview"),
	}})

	got := sourcePaths(t, scope)
	if len(got) != 2 || got[0] != "api/README.md" || got[1] != "web/README.md" {
		t.Fatalf("want both sources kept, got %v", got)
	}
}

func TestAcceptSameTitleAcrossBatchesKeepsBoth(t *testing.T) {
	scope := "test-accept-collide-batch-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	runAccept(t, scope, []any{acceptItem("api/README.md", "Overview")})
	runAccept(t, scope, []any{acceptItem("web/README.md", "Overview")})

	if got := sourcePaths(t, scope); len(got) != 2 {
		t.Fatalf("second accept overwrote the first source's article: %v", got)
	}
}

func TestAcceptResubmitSameSourceReplaces(t *testing.T) {
	scope := "test-accept-resubmit-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	runAccept(t, scope, []any{acceptItem("docs/auth.md", "Auth Flow")})
	runAccept(t, scope, []any{acceptItem("docs/auth.md", "Authentication Overview")})

	arts, err := store.ListArticles(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 1 {
		titles := []string{}
		for _, a := range arts {
			titles = append(titles, a.Title)
		}
		t.Fatalf("re-accepting one source left %d articles %v, want 1", len(arts), titles)
	}
	if arts[0].Title != "Authentication Overview" {
		t.Errorf("title = %q, want the re-compiled one", arts[0].Title)
	}
}

func TestIngestArticleJSONSameTitleDifferentSourcesKeepsBoth(t *testing.T) {
	scope := "test-artjson-collide-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()

	for _, src := range []string{"a/notes.md", "b/notes.md"} {
		payload, _ := json.Marshal(map[string]any{
			"raw_text": "raw " + src,
			"article":  map[string]any{"title": "Meeting Notes", "content": "content " + src, "source": src},
		})
		if err := ingestArticleJSON(scope, payload, true); err != nil {
			t.Fatalf("ingestArticleJSON(%s): %v", src, err)
		}
	}

	arts, err := store.ListArticles(scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(arts) != 2 {
		t.Fatalf("want 2 articles for 2 sources with the same title, got %d", len(arts))
	}
}
