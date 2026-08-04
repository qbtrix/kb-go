// ingest_test.go — Tests for cmdIngest's compile/fallback contract and the
// --article-json external-compile mode.
//
// Covers the loud-fail slice: when LLM compilation fails (no API key here) and
// --allow-fallback is NOT passed, ingest must keep the raw doc, write NO
// article, and surface an error naming the raw doc id and the --allow-fallback
// escape hatch. With --allow-fallback the old verbatim-article behavior is
// restored (CompiledWith "none (fallback)"). The --article-json mode lets an
// external caller supply the compiled article on stdin (raw_text + article),
// so no ANTHROPIC_API_KEY is needed; happy path + field validation are pinned.
//
// Tests target the extracted helpers ingestText / ingestArticleJSON rather
// than cmdIngest itself, because cmdIngest routes errors through fatal()
// (os.Exit) which would abort the test binary. Storage is isolated by
// pointing HOME at t.TempDir(), same pattern as delete_test.go.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tempHomeScope(t *testing.T, prefix string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	scope := prefix + "-" + filepath.Base(dir)
	ensureDirs(scope)
	return scope
}

func wikiArticleCount(t *testing.T, scope string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(scopeDir(scope), "wiki"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func rawDocCount(t *testing.T, scope string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(scopeDir(scope), "raw"))
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			n++
		}
	}
	return n
}

// --- Loud-fail path: compile fails, no --allow-fallback ---

func TestIngestCompileFailureRefusesFallback(t *testing.T) {
	scope := tempHomeScope(t, "ingest-loudfail")

	text := "some raw meeting notes that should not silently become an article"
	// Empty API key guarantees compileLLM fails without any network call.
	err := ingestText(scope, "notes.md", defaultModel, "", "", "", text, false, false)
	if err == nil {
		t.Fatalf("ingestText should return an error when compilation fails and fallback is not allowed")
	}

	// The error must point the caller at the saved raw doc and the escape hatch.
	rawID := contentHash(text)[:16]
	if !strings.Contains(err.Error(), rawID) {
		t.Errorf("error should mention raw doc id %s, got: %v", rawID, err)
	}
	if !strings.Contains(err.Error(), "--allow-fallback") {
		t.Errorf("error should mention --allow-fallback, got: %v", err)
	}

	// Raw content is preserved...
	if got := rawDocCount(t, scope); got != 1 {
		t.Errorf("raw doc count = %d, want 1 (content must not be lost)", got)
	}
	raw, loadErr := loadRawDoc(scope, rawID)
	if loadErr != nil || raw.RawText != text {
		t.Errorf("raw doc %s should round-trip the ingested text", rawID)
	}
	// ...but NO article was written.
	if got := wikiArticleCount(t, scope); got != 0 {
		t.Errorf("wiki article count = %d, want 0 (no silent verbatim fallback)", got)
	}
}

// --- Explicit fallback: --allow-fallback restores the verbatim article ---

func TestIngestAllowFallbackSavesVerbatimArticle(t *testing.T) {
	scope := tempHomeScope(t, "ingest-fallback")

	text := "verbatim raw text stored on explicit request"
	err := ingestText(scope, "notes.md", defaultModel, "", "", "", text, true, false)
	if err != nil {
		t.Fatalf("ingestText with allowFallback should succeed, got: %v", err)
	}

	article, loadErr := loadArticle(scope, slugify("notes.md"))
	if loadErr != nil || article == nil {
		t.Fatalf("fallback article not found: %v", loadErr)
	}
	if article.CompiledWith != "none (fallback)" {
		t.Errorf("CompiledWith = %q, want %q", article.CompiledWith, "none (fallback)")
	}
	if article.Content != text {
		t.Errorf("fallback article content should be the verbatim raw text")
	}
	if len(article.SourceDocs) != 1 || article.SourceDocs[0] != contentHash(text)[:16] {
		t.Errorf("fallback article should link its raw doc, got %v", article.SourceDocs)
	}
}

// --- --article-json: externally compiled article on stdin ---

func TestIngestArticleJSONHappyPath(t *testing.T) {
	scope := tempHomeScope(t, "ingest-artjson")

	payload := `{
		"raw_text": "full raw transcript text here",
		"article": {
			"title": "Auth Session Handling",
			"summary": "How sessions are issued and revoked.",
			"content": "Sessions are issued by the auth service and revoked on logout.",
			"concepts": ["auth", "sessions"],
			"categories": ["Backend"],
			"source": "docs/auth.md",
			"compiled_with": "pocketpaw-backend"
		}
	}`
	if err := ingestArticleJSON(scope, []byte(payload), false); err != nil {
		t.Fatalf("ingestArticleJSON failed: %v", err)
	}

	// Raw doc saved from raw_text and linked from the article.
	rawID := contentHash("full raw transcript text here")[:16]
	raw, err := loadRawDoc(scope, rawID)
	if err != nil || raw.RawText != "full raw transcript text here" {
		t.Fatalf("raw doc %s should be saved from raw_text: %v", rawID, err)
	}
	if raw.Source != "docs/auth.md" {
		t.Errorf("raw doc source = %q, want %q", raw.Source, "docs/auth.md")
	}

	article, err := loadArticle(scope, slugify("Auth Session Handling"))
	if err != nil || article == nil {
		t.Fatalf("article not saved: %v", err)
	}
	if article.CompiledWith != "pocketpaw-backend" {
		t.Errorf("CompiledWith = %q, want caller-supplied %q", article.CompiledWith, "pocketpaw-backend")
	}
	if len(article.SourceDocs) != 1 || article.SourceDocs[0] != rawID {
		t.Errorf("article.SourceDocs = %v, want [%s]", article.SourceDocs, rawID)
	}
	if !contains(article.Concepts, "sessions") {
		t.Errorf("concepts lost in save: %v", article.Concepts)
	}

	// Search index was refreshed and finds the new article.
	all, _ := listArticles(scope)
	si := loadSearchIndex(scope)
	hits := bm25SearchWithIndex(all, "sessions", 5, si)
	found := false
	for _, h := range hits {
		if h.ID == article.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("search should find the externally compiled article")
	}

	// Re-ingesting the same title bumps the version.
	if err := ingestArticleJSON(scope, []byte(payload), false); err != nil {
		t.Fatalf("second ingestArticleJSON failed: %v", err)
	}
	article2, _ := loadArticle(scope, article.ID)
	if article2.Version != 2 {
		t.Errorf("Version = %d after re-ingest, want 2", article2.Version)
	}
}

func TestIngestArticleJSONDefaultsCompiledWithExternal(t *testing.T) {
	scope := tempHomeScope(t, "ingest-artjson-default")

	payload := `{"raw_text": "raw", "article": {"title": "T", "content": "C body"}}`
	if err := ingestArticleJSON(scope, []byte(payload), false); err != nil {
		t.Fatalf("ingestArticleJSON failed: %v", err)
	}
	article, err := loadArticle(scope, slugify("T"))
	if err != nil || article == nil {
		t.Fatalf("article not saved: %v", err)
	}
	if article.CompiledWith != "external" {
		t.Errorf("CompiledWith = %q, want default %q", article.CompiledWith, "external")
	}
}

func TestIngestArticleJSONValidation(t *testing.T) {
	scope := tempHomeScope(t, "ingest-artjson-invalid")

	cases := []struct {
		name    string
		payload string
	}{
		{"bad json", `{not json`},
		{"missing raw_text", `{"article": {"title": "T", "content": "C"}}`},
		{"missing title", `{"raw_text": "r", "article": {"content": "C"}}`},
		{"missing content", `{"raw_text": "r", "article": {"title": "T"}}`},
		{"whitespace title", `{"raw_text": "r", "article": {"title": "  ", "content": "C"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ingestArticleJSON(scope, []byte(tc.payload), false); err == nil {
				t.Errorf("ingestArticleJSON(%s) should fail validation", tc.name)
			}
		})
	}

	// Nothing was written by any of the rejected payloads.
	if got := wikiArticleCount(t, scope); got != 0 {
		t.Errorf("wiki article count = %d after rejected payloads, want 0", got)
	}
}
