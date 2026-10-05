// context_excerpt_test.go — `kb search --context --json`: bodies that contain a
// `---` rule come back as one JSON block each instead of being mis-split. The
// excerpt logic itself is tested in internal/search.

package main

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// Content with a markdown rule ("---" between blank lines) would be split by a
// consumer that splits the text output on the separator; --json keeps it whole.
func TestContextJSONKeepsHorizontalRules(t *testing.T) {
	scope := "test-ctx-json-" + textutil.ContentHash(t.Name())[:8]
	defer func() { os.RemoveAll(store.ScopeDir(scope)) }()
	content := "Intro about the warranty.\n\n---\n\nTail: lifetime repair guarantee on all packs."
	if err := store.SaveArticle(scope, &model.WikiArticle{ID: "warranty", Title: "Warranty", Summary: "Repairs.",
		Content: content, SourcePath: "warranty", Version: 1}); err != nil {
		t.Fatal(err)
	}
	out := runSearchContext(t, "warranty repair", "--scope", scope, "--limit", "3", "--json")
	var got []struct {
		ID        string `json:"id"`
		Title     string `json:"title"`
		Text      string `json:"text"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if len(got) != 1 || got[0].ID != "warranty" || got[0].Title != "Warranty" || got[0].Truncated {
		t.Fatalf("unexpected entries: %+v", got)
	}
	if got[0].Text != content {
		t.Errorf("text not intact:\n%q\nwant\n%q", got[0].Text, content)
	}
}
