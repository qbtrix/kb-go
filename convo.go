// convo.go — Conversation mode for kb-go.
// Parses conversation transcripts, extracts entities/decisions/topics,
// generates wiki articles per topic cluster. Hooks into existing BM25 search.
//
// Commands: kb convo ingest <file>, kb convo search <query>, kb convo list
// Formats:  JSON array, JSONL, plain text (Speaker: message)
// Extraction: deterministic NER + decision/preference pattern matching, no LLM needed.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// --- Data models ---

// ConvoTurn is a single turn in a conversation.
type ConvoTurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	Index   int    `json:"index"`
}

// ConvoSession is a parsed conversation with extracted metadata.
type ConvoSession struct {
	ID       string      `json:"id"`
	Source   string      `json:"source"`
	Turns    []ConvoTurn `json:"turns"`
	ParsedAt string      `json:"parsed_at"`
}

// ExtractedEntity is a named entity found in conversation text.
type ExtractedEntity struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // person, technology, project, organization, unknown
	Count int    `json:"count"`
}

// ExtractedDecision is a decision or preference found in text.
type ExtractedDecision struct {
	Text      string `json:"text"`
	Type      string `json:"type"` // decision, preference, event
	TurnIndex int    `json:"turn_index"`
}

// TopicCluster groups related turns by shared entities/topics.
type TopicCluster struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Entities []string `json:"entities"`
	TurnIdxs []int    `json:"turn_idxs"`
}

// --- Transcript parsing ---

// parseTranscript auto-detects format and parses into turns.
func parseTranscript(data []byte, source string) (*ConvoSession, error) {
	text := strings.TrimSpace(string(data))

	var turns []ConvoTurn

	// Try JSON array first
	if strings.HasPrefix(text, "[") {
		var msgs []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			// ChatGPT format
			Author struct {
				Role string `json:"role"`
			} `json:"author"`
			Parts []string `json:"parts"`
		}
		if err := json.Unmarshal([]byte(text), &msgs); err == nil && len(msgs) > 0 {
			for i, m := range msgs {
				role := m.Role
				content := m.Content
				if role == "" && m.Author.Role != "" {
					role = m.Author.Role
				}
				if content == "" && len(m.Parts) > 0 {
					content = strings.Join(m.Parts, "\n")
				}
				if role == "" || content == "" {
					continue
				}
				turns = append(turns, ConvoTurn{Role: normalizeRole(role), Content: content, Index: i})
			}
			if len(turns) > 0 {
				return makeSession(turns, source), nil
			}
		}
	}

	// Try JSONL
	if strings.HasPrefix(text, "{") {
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var m struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if err := json.Unmarshal([]byte(line), &m); err == nil && m.Role != "" && m.Content != "" {
				turns = append(turns, ConvoTurn{Role: normalizeRole(m.Role), Content: m.Content, Index: i})
			}
		}
		if len(turns) > 0 {
			return makeSession(turns, source), nil
		}
	}

	// Plain text: "Speaker: message" separated by blank lines or newlines
	turns = parsePlainText(text)
	if len(turns) > 0 {
		return makeSession(turns, source), nil
	}

	return nil, fmt.Errorf("could not parse transcript: no recognized format")
}

var plainTextRoleRe = regexp.MustCompile(`(?i)^(user|assistant|human|ai|system|claude|gpt|bot|agent|you|me)\s*:\s*`)

func parsePlainText(text string) []ConvoTurn {
	var turns []ConvoTurn
	var currentRole string
	var currentContent strings.Builder
	idx := 0

	flush := func() {
		if currentRole != "" && currentContent.Len() > 0 {
			turns = append(turns, ConvoTurn{
				Role:    normalizeRole(currentRole),
				Content: strings.TrimSpace(currentContent.String()),
				Index:   idx,
			})
			idx++
		}
		currentContent.Reset()
	}

	for _, line := range strings.Split(text, "\n") {
		if match := plainTextRoleRe.FindStringSubmatch(line); len(match) > 1 {
			flush()
			currentRole = match[1]
			rest := plainTextRoleRe.ReplaceAllString(line, "")
			currentContent.WriteString(rest)
		} else if strings.TrimSpace(line) == "" && currentRole != "" {
			// Blank line might separate turns, but only flush if next line starts a new role
			currentContent.WriteString("\n")
		} else {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}
	flush()
	return turns
}

func normalizeRole(role string) string {
	r := strings.ToLower(strings.TrimSpace(role))
	switch r {
	case "human", "me", "you":
		return "user"
	case "ai", "claude", "gpt", "bot", "agent":
		return "assistant"
	default:
		return r
	}
}

func makeSession(turns []ConvoTurn, source string) *ConvoSession {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", source, len(turns), time.Now().String())))
	return &ConvoSession{
		ID:       fmt.Sprintf("convo-%x", h[:8]),
		Source:   source,
		Turns:    turns,
		ParsedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// --- Entity extraction (deterministic, no LLM) ---

// Common technology names that should be recognized even in lowercase.
var techNames = map[string]bool{
	"python": true, "go": true, "golang": true, "rust": true, "java": true,
	"javascript": true, "typescript": true, "ruby": true, "swift": true, "kotlin": true,
	"react": true, "vue": true, "svelte": true, "angular": true, "nextjs": true,
	"fastapi": true, "django": true, "flask": true, "express": true, "rails": true,
	"postgres": true, "postgresql": true, "mysql": true, "sqlite": true, "mongodb": true,
	"redis": true, "kafka": true, "rabbitmq": true, "elasticsearch": true,
	"docker": true, "kubernetes": true, "terraform": true, "aws": true, "gcp": true, "azure": true,
	"graphql": true, "grpc": true, "rest": true, "websocket": true,
	"git": true, "github": true, "gitlab": true, "jira": true, "linear": true,
	"claude": true, "openai": true, "anthropic": true, "gemini": true, "llama": true,
	"chromadb": true, "pinecone": true, "weaviate": true, "qdrant": true,
	"oauth": true, "jwt": true, "ssl": true, "tls": true, "ssh": true,
	"nginx": true, "caddy": true, "traefik": true,
	"pydantic": true, "sqlalchemy": true, "prisma": true, "drizzle": true,
	"sveltekit": true, "tauri": true, "electron": true,
}

// Words to skip during proper noun extraction.
var commonWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"is": true, "are": true, "was": true, "were": true, "be": true, "been": true,
	"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true, "might": true,
	"can": true, "shall": true, "must": true,
	"for": true, "of": true, "to": true, "in": true, "on": true, "at": true,
	"by": true, "with": true, "from": true, "about": true, "into": true,
	"this": true, "that": true, "these": true, "those": true,
	"i": true, "we": true, "you": true, "they": true, "he": true, "she": true, "it": true,
	"my": true, "our": true, "your": true, "their": true, "its": true,
	"not": true, "no": true, "yes": true, "so": true, "if": true, "then": true,
	"what": true, "when": true, "where": true, "who": true, "why": true, "how": true,
	"also": true, "just": true, "like": true, "very": true, "really": true,
	"here": true, "there": true, "now": true, "well": true,
	"sure": true, "yeah": true, "okay": true, "ok": true, "got": true,
	"let": true, "using": true, "used": true, "use": true, "need": true,
	"want": true, "think": true, "know": true, "make": true, "take": true,
	"get": true, "see": true, "look": true, "try": true, "going": true,
	// Sentence starters that are often capitalized but not entities
	"however": true, "therefore": true, "meanwhile": true, "actually": true,
	"basically": true, "currently": true, "originally": true, "finally": true,
	"maybe": true, "perhaps": true, "probably": true, "definitely": true,
	"thanks": true, "great": true, "right": true, "nice": true, "good": true,
}

// extractEntities finds named entities in text using heuristics.
func extractEntities(text string) []ExtractedEntity {
	counts := map[string]string{} // name -> type
	freq := map[string]int{}

	words := strings.Fields(text)

	for i, word := range words {
		clean := strings.Trim(word, ".,;:!?\"'()[]{}")

		// Technology names (case-insensitive match)
		lower := strings.ToLower(clean)
		if techNames[lower] && len(clean) > 1 {
			canonical := canonicalTechName(lower)
			counts[canonical] = "technology"
			freq[canonical]++
			continue
		}

		// Proper nouns: capitalized words not at sentence start, not common words
		if len(clean) >= 2 && unicode.IsUpper(rune(clean[0])) && !commonWords[lower] {
			// Skip if it's the first word after sentence-ending punctuation
			atSentenceStart := i == 0
			if i > 0 {
				prev := strings.Trim(words[i-1], "\"'")
				if strings.HasSuffix(prev, ".") || strings.HasSuffix(prev, "!") || strings.HasSuffix(prev, "?") {
					atSentenceStart = true
				}
			}

			if !atSentenceStart {
				// Check for multi-word proper nouns (e.g., "Acme Corp", "New York")
				entity := clean
				for j := i + 1; j < len(words) && j < i+4; j++ {
					next := strings.Trim(words[j], ".,;:!?\"'()[]{}")
					nextLower := strings.ToLower(next)
					if len(next) >= 2 && unicode.IsUpper(rune(next[0])) && !commonWords[nextLower] {
						entity += " " + next
					} else {
						break
					}
				}
				if !commonWords[strings.ToLower(entity)] {
					eType := guessEntityType(entity)
					counts[entity] = eType
					freq[entity]++
				}
			}
		}

		// Uppercase acronyms (API, SQL, HTTP, etc.) — 2-6 chars, all upper
		if len(clean) >= 2 && len(clean) <= 6 && clean == strings.ToUpper(clean) && isAlpha(clean) {
			if !commonWords[lower] {
				counts[clean] = "technology"
				freq[clean]++
			}
		}
	}

	var entities []ExtractedEntity
	for name, typ := range counts {
		entities = append(entities, ExtractedEntity{Name: name, Type: typ, Count: freq[name]})
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].Count > entities[j].Count })
	return entities
}

func canonicalTechName(lower string) string {
	canonical := map[string]string{
		"golang": "Go", "postgresql": "Postgres", "nextjs": "Next.js",
		"sveltekit": "SvelteKit", "graphql": "GraphQL", "grpc": "gRPC",
		"openai": "OpenAI", "chromadb": "ChromaDB", "fastapi": "FastAPI",
		"mongodb": "MongoDB", "rabbitmq": "RabbitMQ", "elasticsearch": "Elasticsearch",
		"mysql": "MySQL", "sqlite": "SQLite", "javascript": "JavaScript",
		"typescript": "TypeScript", "kubernetes": "Kubernetes",
		"sqlalchemy": "SQLAlchemy", "websocket": "WebSocket",
	}
	if c, ok := canonical[lower]; ok {
		return c
	}
	// Title case for most tech names
	if len(lower) <= 3 {
		return strings.ToUpper(lower) // AWS, GCP, JWT, SSL
	}
	return strings.ToUpper(lower[:1]) + lower[1:]
}

func guessEntityType(name string) string {
	lower := strings.ToLower(name)
	// Suffixes that suggest organization
	if strings.HasSuffix(lower, " corp") || strings.HasSuffix(lower, " inc") ||
		strings.HasSuffix(lower, " ltd") || strings.HasSuffix(lower, " co") ||
		strings.HasSuffix(lower, " labs") || strings.HasSuffix(lower, " systems") {
		return "organization"
	}
	// Single capitalized word is likely a person or project
	if !strings.Contains(name, " ") {
		return "unknown" // could be person or project
	}
	return "unknown"
}

func isAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// --- Decision/preference extraction ---

var decisionPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(decided|choosing|chose|picked|selected|going with|switched to|migrated to|moving to|adopting)\b`),
	regexp.MustCompile(`(?i)\b(we('ll| will) use|let's use|let's go with|we('re| are) using|I('ll| will) use)\b`),
	regexp.MustCompile(`(?i)\b(the plan is to|the approach is|our strategy is|we('re| are) going to)\b`),
}

var preferencePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(I prefer|I like|I want|I need|I('d| would) rather|my preference is)\b`),
	regexp.MustCompile(`(?i)\b(please (always|never|don't))\b`),
	regexp.MustCompile(`(?i)\b(I('m| am) (a|an)\s+\w+)\b`), // "I'm a data scientist"
}

var eventPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(shipped|deployed|launched|released|merged|completed|finished|fixed)\b`),
	regexp.MustCompile(`(?i)\b(yesterday|today|last (week|month|sprint)|this (morning|afternoon))\b`),
}

func extractDecisions(turns []ConvoTurn) []ExtractedDecision {
	var decisions []ExtractedDecision
	for _, turn := range turns {
		if turn.Role != "user" {
			continue // Decisions come from the user, not the assistant
		}
		sentences := splitSentences(turn.Content)
		for _, sent := range sentences {
			for _, pat := range decisionPatterns {
				if pat.MatchString(sent) {
					decisions = append(decisions, ExtractedDecision{
						Text: strings.TrimSpace(sent), Type: "decision", TurnIndex: turn.Index,
					})
					break
				}
			}
			for _, pat := range preferencePatterns {
				if pat.MatchString(sent) {
					decisions = append(decisions, ExtractedDecision{
						Text: strings.TrimSpace(sent), Type: "preference", TurnIndex: turn.Index,
					})
					break
				}
			}
			for _, pat := range eventPatterns {
				if pat.MatchString(sent) {
					decisions = append(decisions, ExtractedDecision{
						Text: strings.TrimSpace(sent), Type: "event", TurnIndex: turn.Index,
					})
					break
				}
			}
		}
	}
	return decisions
}

func splitSentences(text string) []string {
	// Simple sentence splitter: split on .!? followed by space or end
	re := regexp.MustCompile(`[.!?]+\s+|\n`)
	parts := re.Split(text, -1)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if len(p) > 10 { // Skip very short fragments
			out = append(out, p)
		}
	}
	return out
}

// --- Topic clustering (entity overlap) ---

func clusterTopics(session *ConvoSession) []TopicCluster {
	// Extract entities per turn
	turnEntities := make([]map[string]bool, len(session.Turns))
	for i, turn := range session.Turns {
		entities := extractEntities(turn.Content)
		m := map[string]bool{}
		for _, e := range entities {
			m[strings.ToLower(e.Name)] = true
		}
		turnEntities[i] = m
	}

	// Greedy clustering: turns share a cluster if they share >= 1 entity
	assigned := make([]int, len(session.Turns)) // cluster ID per turn, -1 = unassigned
	for i := range assigned {
		assigned[i] = -1
	}

	clusterID := 0
	for i := range session.Turns {
		if assigned[i] >= 0 {
			continue
		}
		// Start a new cluster from this turn
		assigned[i] = clusterID
		// Find all turns that share entities with this cluster
		clusterEntities := copySet(turnEntities[i])
		changed := true
		for changed {
			changed = false
			for j := range session.Turns {
				if assigned[j] >= 0 {
					continue
				}
				if setsOverlap(clusterEntities, turnEntities[j]) {
					assigned[j] = clusterID
					for e := range turnEntities[j] {
						clusterEntities[e] = true
					}
					changed = true
				}
			}
		}
		clusterID++
	}

	// Build clusters
	clusterMap := map[int]*TopicCluster{}
	for i, cid := range assigned {
		if cid < 0 {
			// Unassigned turn (no entities) — put in a "general" cluster
			cid = -1
			if _, ok := clusterMap[-1]; !ok {
				clusterMap[-1] = &TopicCluster{ID: "general", Label: "General Discussion"}
			}
		}
		tc, ok := clusterMap[cid]
		if !ok {
			tc = &TopicCluster{}
			clusterMap[cid] = tc
		}
		tc.TurnIdxs = append(tc.TurnIdxs, i)
	}

	// Label clusters by most frequent entity
	for cid, tc := range clusterMap {
		if cid < 0 {
			continue
		}
		entityFreq := map[string]int{}
		for _, idx := range tc.TurnIdxs {
			for e := range turnEntities[idx] {
				entityFreq[e]++
			}
		}
		// Sort by frequency
		type ef struct {
			name string
			freq int
		}
		var sorted []ef
		for n, f := range entityFreq {
			sorted = append(sorted, ef{n, f})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].freq > sorted[j].freq })

		for _, s := range sorted {
			tc.Entities = append(tc.Entities, s.name)
		}

		if len(sorted) > 0 {
			tc.Label = sorted[0].name
			tc.ID = slugify(sorted[0].name)
		} else {
			tc.Label = fmt.Sprintf("topic-%d", cid)
			tc.ID = fmt.Sprintf("topic-%d", cid)
		}
	}

	var clusters []TopicCluster
	for _, tc := range clusterMap {
		clusters = append(clusters, *tc)
	}
	sort.Slice(clusters, func(i, j int) bool {
		return len(clusters[i].TurnIdxs) > len(clusters[j].TurnIdxs)
	})
	return clusters
}

func copySet(s map[string]bool) map[string]bool {
	out := make(map[string]bool, len(s))
	for k := range s {
		out[k] = true
	}
	return out
}

func setsOverlap(a, b map[string]bool) bool {
	for k := range a {
		if b[k] {
			return true
		}
	}
	return false
}

// --- Article generation from conversation ---

func generateConvoArticles(session *ConvoSession, clusters []TopicCluster, decisions []ExtractedDecision) []*WikiArticle {
	var articles []*WikiArticle

	// Build a decision lookup by turn index
	decByTurn := map[int][]ExtractedDecision{}
	for _, d := range decisions {
		decByTurn[d.TurnIndex] = append(decByTurn[d.TurnIndex], d)
	}

	for _, cluster := range clusters {
		var content strings.Builder

		// Verbatim turns (the "drawers")
		content.WriteString("## Conversation\n\n")
		for _, idx := range cluster.TurnIdxs {
			if idx >= len(session.Turns) {
				continue
			}
			turn := session.Turns[idx]
			content.WriteString(fmt.Sprintf("**%s:** %s\n\n", strings.Title(turn.Role), turn.Content))
		}

		// Extracted decisions for this cluster
		var clusterDecisions []ExtractedDecision
		for _, idx := range cluster.TurnIdxs {
			clusterDecisions = append(clusterDecisions, decByTurn[idx]...)
		}
		if len(clusterDecisions) > 0 {
			content.WriteString("## Extracted\n\n")
			for _, d := range clusterDecisions {
				content.WriteString(fmt.Sprintf("- [%s] %s\n", d.Type, d.Text))
			}
			content.WriteString("\n")
		}

		// Summary line
		summary := fmt.Sprintf("Conversation about %s (%d turns)", cluster.Label, len(cluster.TurnIdxs))

		// Concepts = cluster entities
		concepts := cluster.Entities
		if len(concepts) > 10 {
			concepts = concepts[:10]
		}

		articleID := fmt.Sprintf("convo-%s-%s", session.ID[:12], cluster.ID)
		if len(articleID) > 60 {
			articleID = articleID[:60]
		}

		articles = append(articles, &WikiArticle{
			ID:           articleID,
			Title:        fmt.Sprintf("%s (conversation)", cluster.Label),
			Summary:      summary,
			Content:      content.String(),
			Concepts:     concepts,
			Categories:   []string{"conversation"},
			SourceDocs:   []string{session.ID},
			WordCount:    wordCount(content.String()),
			CompiledAt:   time.Now().UTC().Format(time.RFC3339),
			CompiledWith: "kb-convo-deterministic",
			Version:      1,
		})
	}

	return articles
}

// --- CLI commands ---

func cmdConvo(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: kb convo <ingest|search|list> [options]")
		os.Exit(1)
	}

	sub := args[0]
	subArgs := args[1:]

	switch sub {
	case "ingest":
		cmdConvoIngest(subArgs)
	case "search":
		cmdConvoSearch(subArgs)
	case "list":
		cmdConvoList(subArgs)
	default:
		fmt.Fprintf(os.Stderr, "Unknown convo subcommand: %s\n", sub)
		os.Exit(1)
	}
}

func cmdConvoIngest(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	// Find the file path (first non-flag argument)
	filePath := firstNonFlag(args)
	if filePath == "" {
		fatal("Usage: kb convo ingest <file> [--scope NAME] [--json]")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		fatal("Cannot read file: %v", err)
	}

	session, err := parseTranscript(data, filePath)
	if err != nil {
		fatal("Parse error: %v", err)
	}

	// Extract entities and decisions
	allText := ""
	for _, t := range session.Turns {
		allText += t.Content + " "
	}
	entities := extractEntities(allText)
	decisions := extractDecisions(session.Turns)

	// Cluster into topics
	clusters := clusterTopics(session)

	// Generate articles
	articles := generateConvoArticles(session, clusters, decisions)

	// Save raw session
	ensureDirs(scope)
	rawPath := filepath.Join(scopeDir(scope), "raw", session.ID+".json")
	rawData, _ := json.MarshalIndent(session, "", "  ")
	os.WriteFile(rawPath, rawData, 0o644)

	// Save articles
	for _, a := range articles {
		if err := saveArticle(scope, a); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: save article %s: %v\n", a.ID, err)
		}
	}

	if jsonOut {
		out := map[string]any{
			"session_id": session.ID,
			"turns":      len(session.Turns),
			"entities":   entities,
			"decisions":  decisions,
			"clusters":   len(clusters),
			"articles":   len(articles),
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
	} else {
		fmt.Printf("Ingested %d turns from %s\n", len(session.Turns), filepath.Base(filePath))
		fmt.Printf("  Entities:  %d found\n", len(entities))
		fmt.Printf("  Decisions: %d extracted\n", len(decisions))
		fmt.Printf("  Topics:    %d clusters\n", len(clusters))
		fmt.Printf("  Articles:  %d created\n", len(articles))
		fmt.Printf("  Session:   %s\n", session.ID)
		if len(entities) > 0 {
			fmt.Printf("  Top entities: ")
			limit := 5
			if len(entities) < limit {
				limit = len(entities)
			}
			names := make([]string, limit)
			for i := 0; i < limit; i++ {
				names[i] = entities[i].Name
			}
			fmt.Println(strings.Join(names, ", "))
		}
	}
}

func cmdConvoSearch(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")
	limit := 10

	query := firstNonFlag(args)
	if query == "" {
		fatal("Usage: kb convo search <query> [--scope NAME] [--json]")
	}

	articles, err := listArticles(scope)
	if err != nil {
		fatal("Cannot load articles: %v", err)
	}

	// Filter to conversation articles only
	var convoArticles []*WikiArticle
	for _, a := range articles {
		for _, cat := range a.Categories {
			if cat == "conversation" {
				convoArticles = append(convoArticles, a)
				break
			}
		}
	}

	if len(convoArticles) == 0 {
		if jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("No conversation articles found. Run 'kb convo ingest <file>' first.")
		}
		return
	}

	results := bm25Search(convoArticles, query, limit)
	if jsonOut {
		type result struct {
			ID      string   `json:"id"`
			Title   string   `json:"title"`
			Summary string   `json:"summary"`
			Concepts []string `json:"concepts"`
		}
		var out []result
		for _, r := range results {
			out = append(out, result{ID: r.ID, Title: r.Title, Summary: r.Summary, Concepts: r.Concepts})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
	} else {
		for i, r := range results {
			fmt.Printf("%d. %s\n   %s\n", i+1, r.Title, r.Summary)
			if len(r.Concepts) > 0 {
				fmt.Printf("   Concepts: %s\n", strings.Join(r.Concepts, ", "))
			}
			fmt.Println()
		}
	}
}

func cmdConvoList(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	articles, err := listArticles(scope)
	if err != nil {
		fatal("Cannot load articles: %v", err)
	}

	var convoArticles []*WikiArticle
	for _, a := range articles {
		for _, cat := range a.Categories {
			if cat == "conversation" {
				convoArticles = append(convoArticles, a)
				break
			}
		}
	}

	if jsonOut {
		type item struct {
			ID      string   `json:"id"`
			Title   string   `json:"title"`
			Summary string   `json:"summary"`
			Concepts []string `json:"concepts"`
		}
		var out []item
		for _, a := range convoArticles {
			out = append(out, item{ID: a.ID, Title: a.Title, Summary: a.Summary, Concepts: a.Concepts})
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(out)
	} else {
		fmt.Printf("%d conversation articles\n\n", len(convoArticles))
		for _, a := range convoArticles {
			fmt.Printf("  %s — %s\n", a.ID, a.Title)
		}
	}
}

// firstNonFlag returns the first argument that doesn't start with --.
func firstNonFlag(args []string) string {
	flagsWithValues := map[string]bool{"--scope": true, "--model": true, "--source": true}
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if flagsWithValues[a] {
			skipNext = true
			continue
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		return a
	}
	return ""
}
