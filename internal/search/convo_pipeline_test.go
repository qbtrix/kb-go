// convo_pipeline_test.go — End to end: a transcript goes through the convo
// extractors into stored articles, and BM25 retrieves them by topic.

package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/convo"
	"github.com/qbtrix/kb-go/internal/kbtest"
)

func TestConvoPipeline_EndToEnd(t *testing.T) {
	dir := kbtest.IsolatedHome(t)

	// Create a test transcript
	transcript := `[
		{"role": "user", "content": "I'm Marcus, a backend engineer at Acme Corp. I mainly work with Python and FastAPI."},
		{"role": "assistant", "content": "Nice to meet you, Marcus! Python and FastAPI are great choices."},
		{"role": "user", "content": "We decided to migrate our auth to Clerk because of better developer experience."},
		{"role": "assistant", "content": "Clerk is a solid choice for modern auth."},
		{"role": "user", "content": "I also use Docker and Kubernetes for deployment. We shipped v2.0 yesterday."},
		{"role": "assistant", "content": "Congrats on the v2.0 launch!"},
		{"role": "user", "content": "I prefer using vim over VS Code for quick edits."},
		{"role": "assistant", "content": "Vim is great for speed."}
	]`

	txFile := filepath.Join(dir, "test_convo.json")
	os.WriteFile(txFile, []byte(transcript), 0o644)

	// Parse
	data, _ := os.ReadFile(txFile)
	session, err := convo.ParseTranscript(data, txFile)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(session.Turns) != 8 {
		t.Fatalf("want 8 turns, got %d", len(session.Turns))
	}

	// Extract
	allText := ""
	for _, turn := range session.Turns {
		allText += turn.Content + " "
	}
	entities := convo.ExtractEntities(allText)
	decisions := convo.ExtractDecisions(session.Turns)

	// Verify entity extraction
	entityNames := map[string]bool{}
	for _, e := range entities {
		entityNames[e.Name] = true
	}
	for _, want := range []string{"Python", "FastAPI", "Docker", "Kubernetes"} {
		if !entityNames[want] {
			t.Errorf("missing entity: %s (found: %v)", want, entities)
		}
	}

	// Verify decision extraction
	if len(decisions) == 0 {
		t.Error("expected decisions extracted from 'decided to migrate' and 'I prefer'")
	}

	// Cluster and generate articles
	clusters := convo.ClusterTopics(session)
	articles := convo.GenerateArticles(session, clusters, decisions)

	if len(articles) == 0 {
		t.Fatal("expected articles generated from transcript")
	}

	// Verify articles are searchable via BM25
	results := BM25(articles, "auth migration Clerk", 5)
	if len(results) == 0 {
		t.Error("BM25 search for 'auth migration Clerk' should return results")
	}

	// Verify JSON round-trip
	for _, a := range articles {
		data, err := json.Marshal(a)
		if err != nil {
			t.Errorf("marshal article %s: %v", a.ID, err)
		}
		if len(data) == 0 {
			t.Errorf("empty JSON for article %s", a.ID)
		}
	}
}
