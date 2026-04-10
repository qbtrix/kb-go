// convo_test.go — Tests for conversation mode.
// Covers transcript parsing (JSON, JSONL, plain text), entity extraction,
// decision/preference extraction, topic clustering, and article generation.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// --- Transcript Parsing ---

func TestParseTranscript_JSONArray(t *testing.T) {
	data := `[
		{"role": "user", "content": "I'm building an auth service with OAuth2"},
		{"role": "assistant", "content": "OAuth2 is a good choice for modern auth."},
		{"role": "user", "content": "We decided to use Clerk over Auth0"}
	]`
	session, err := parseTranscript([]byte(data), "test.json")
	if err != nil {
		t.Fatalf("parse JSON array: %v", err)
	}
	if len(session.Turns) != 3 {
		t.Errorf("want 3 turns, got %d", len(session.Turns))
	}
	if session.Turns[0].Role != "user" {
		t.Errorf("first turn role: want 'user', got '%s'", session.Turns[0].Role)
	}
}

func TestParseTranscript_JSONL(t *testing.T) {
	data := `{"role": "human", "content": "Hello"}
{"role": "ai", "content": "Hi there!"}
{"role": "human", "content": "What's Go?"}`
	session, err := parseTranscript([]byte(data), "test.jsonl")
	if err != nil {
		t.Fatalf("parse JSONL: %v", err)
	}
	if len(session.Turns) != 3 {
		t.Errorf("want 3 turns, got %d", len(session.Turns))
	}
	// "human" should normalize to "user"
	if session.Turns[0].Role != "user" {
		t.Errorf("human should normalize to user, got '%s'", session.Turns[0].Role)
	}
	// "ai" should normalize to "assistant"
	if session.Turns[1].Role != "assistant" {
		t.Errorf("ai should normalize to assistant, got '%s'", session.Turns[1].Role)
	}
}

func TestParseTranscript_PlainText(t *testing.T) {
	data := `User: I use Python for data analysis
Assistant: Python is great for that.
User: We're switching to Rust for the backend`
	session, err := parseTranscript([]byte(data), "test.txt")
	if err != nil {
		t.Fatalf("parse plain text: %v", err)
	}
	if len(session.Turns) != 3 {
		t.Errorf("want 3 turns, got %d", len(session.Turns))
	}
}

func TestParseTranscript_BadInput(t *testing.T) {
	_, err := parseTranscript([]byte(""), "empty")
	if err == nil {
		t.Error("empty input should fail")
	}
}

func TestNormalizeRole(t *testing.T) {
	cases := map[string]string{
		"human": "user", "Human": "user", "me": "user", "you": "user",
		"ai": "assistant", "claude": "assistant", "gpt": "assistant", "bot": "assistant",
		"user": "user", "assistant": "assistant", "system": "system",
	}
	for input, want := range cases {
		got := normalizeRole(input)
		if got != want {
			t.Errorf("normalizeRole(%q): want %q, got %q", input, want, got)
		}
	}
}

// --- Entity Extraction ---

func TestExtractEntities_TechNames(t *testing.T) {
	text := "We're using Python and FastAPI with Postgres for the backend"
	entities := extractEntities(text)

	found := map[string]bool{}
	for _, e := range entities {
		found[e.Name] = true
	}

	for _, want := range []string{"Python", "FastAPI", "Postgres"} {
		if !found[want] {
			t.Errorf("expected entity %q, found: %v", want, entityNames(entities))
		}
	}
}

func TestExtractEntities_ProperNouns(t *testing.T) {
	text := "I talked to Marcus about the Orion project yesterday"
	entities := extractEntities(text)

	found := map[string]bool{}
	for _, e := range entities {
		found[e.Name] = true
	}

	if !found["Marcus"] && !found["Orion"] {
		t.Errorf("expected Marcus or Orion, found: %v", entityNames(entities))
	}
}

func TestExtractEntities_Acronyms(t *testing.T) {
	text := "The API uses JWT tokens over SSL"
	entities := extractEntities(text)

	found := map[string]bool{}
	for _, e := range entities {
		found[e.Name] = true
	}

	for _, want := range []string{"API", "JWT", "SSL"} {
		if !found[want] {
			t.Errorf("expected acronym %q, found: %v", want, entityNames(entities))
		}
	}
}

func TestExtractEntities_Empty(t *testing.T) {
	entities := extractEntities("")
	if len(entities) != 0 {
		t.Errorf("empty input should yield no entities, got %d", len(entities))
	}
}

func TestExtractEntities_SkipsCommonWords(t *testing.T) {
	text := "the and but or is are was were"
	entities := extractEntities(text)
	if len(entities) != 0 {
		t.Errorf("common words should yield no entities, got: %v", entityNames(entities))
	}
}

// --- Decision Extraction ---

func TestExtractDecisions_Decisions(t *testing.T) {
	turns := []ConvoTurn{
		{Role: "user", Content: "We decided to use Clerk over Auth0 for authentication.", Index: 0},
		{Role: "assistant", Content: "Good choice.", Index: 1},
		{Role: "user", Content: "I'm switching to GraphQL for the API layer.", Index: 2},
	}
	decisions := extractDecisions(turns)

	decisionTexts := make([]string, len(decisions))
	for i, d := range decisions {
		decisionTexts[i] = d.Text
	}

	if len(decisions) == 0 {
		t.Fatal("expected at least 1 decision extracted")
	}

	foundDecision := false
	for _, d := range decisions {
		if d.Type == "decision" {
			foundDecision = true
		}
	}
	if !foundDecision {
		t.Errorf("expected a 'decision' type, got types: %v", decisionTexts)
	}
}

func TestExtractDecisions_Preferences(t *testing.T) {
	turns := []ConvoTurn{
		{Role: "user", Content: "I prefer dark mode and I'd rather use vim than VS Code.", Index: 0},
	}
	decisions := extractDecisions(turns)

	foundPref := false
	for _, d := range decisions {
		if d.Type == "preference" {
			foundPref = true
		}
	}
	if !foundPref {
		t.Error("expected a 'preference' type extraction")
	}
}

func TestExtractDecisions_Events(t *testing.T) {
	turns := []ConvoTurn{
		{Role: "user", Content: "We shipped the new auth service yesterday.", Index: 0},
	}
	decisions := extractDecisions(turns)

	foundEvent := false
	for _, d := range decisions {
		if d.Type == "event" {
			foundEvent = true
		}
	}
	if !foundEvent {
		t.Error("expected an 'event' type extraction")
	}
}

func TestExtractDecisions_SkipsAssistantTurns(t *testing.T) {
	turns := []ConvoTurn{
		{Role: "assistant", Content: "We decided to use Postgres.", Index: 0},
	}
	decisions := extractDecisions(turns)
	if len(decisions) != 0 {
		t.Error("should not extract decisions from assistant turns")
	}
}

// --- Topic Clustering ---

func TestClusterTopics_SharedEntities(t *testing.T) {
	session := &ConvoSession{
		ID: "test",
		Turns: []ConvoTurn{
			{Role: "user", Content: "Let's discuss Python and FastAPI", Index: 0},
			{Role: "assistant", Content: "Python with FastAPI is great", Index: 1},
			{Role: "user", Content: "Now about Kubernetes deployment", Index: 2},
			{Role: "assistant", Content: "Kubernetes works well with Docker", Index: 3},
		},
	}
	clusters := clusterTopics(session)

	// Should have at least 2 clusters: Python/FastAPI and Kubernetes/Docker
	if len(clusters) < 2 {
		t.Errorf("expected at least 2 topic clusters, got %d", len(clusters))
	}
}

func TestClusterTopics_EmptySession(t *testing.T) {
	session := &ConvoSession{ID: "empty", Turns: nil}
	clusters := clusterTopics(session)
	if len(clusters) != 0 {
		t.Errorf("empty session should yield 0 clusters, got %d", len(clusters))
	}
}

// --- Article Generation ---

func TestGenerateConvoArticles_CreatesArticles(t *testing.T) {
	session := &ConvoSession{
		ID:     "convo-test12345678",
		Source: "test.json",
		Turns: []ConvoTurn{
			{Role: "user", Content: "We chose Postgres for the database", Index: 0},
			{Role: "assistant", Content: "Postgres is solid.", Index: 1},
		},
	}
	clusters := clusterTopics(session)
	decisions := extractDecisions(session.Turns)
	articles := generateConvoArticles(session, clusters, decisions)

	if len(articles) == 0 {
		t.Fatal("expected at least 1 article generated")
	}

	// All articles should be categorized as "conversation"
	for _, a := range articles {
		found := false
		for _, cat := range a.Categories {
			if cat == "conversation" {
				found = true
			}
		}
		if !found {
			t.Errorf("article %s missing 'conversation' category", a.ID)
		}
	}
}

// --- Integration: full pipeline ---

func TestConvoPipeline_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	// Override the KB base dir for this test
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", dir)
	defer os.Setenv("HOME", origHome)

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
	session, err := parseTranscript(data, txFile)
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
	entities := extractEntities(allText)
	decisions := extractDecisions(session.Turns)

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
	clusters := clusterTopics(session)
	articles := generateConvoArticles(session, clusters, decisions)

	if len(articles) == 0 {
		t.Fatal("expected articles generated from transcript")
	}

	// Verify articles are searchable via BM25
	results := bm25Search(articles, "auth migration Clerk", 5)
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

// --- Helpers ---

func entityNames(entities []ExtractedEntity) []string {
	names := make([]string, len(entities))
	for i, e := range entities {
		names[i] = e.Name
	}
	return names
}
