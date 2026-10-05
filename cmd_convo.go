// Implements `kb convo {ingest,search,list}`: reads a transcript, runs the
// conversation extractors, writes the session raw doc and per-topic articles,
// and searches/lists conversation articles.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/search"
	"github.com/qbtrix/kb-go/internal/store"
)

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
	store.EnsureDirs(scope)
	rawPath := filepath.Join(store.ScopeDir(scope), "raw", session.ID+".json")
	rawData, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		fatal("Marshal session: %v", err)
	}
	if err := os.WriteFile(rawPath, rawData, 0o644); err != nil {
		fatal("Save raw session: %v", err)
	}

	// Save articles
	for _, a := range articles {
		if err := store.SaveArticle(scope, a); err != nil {
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

	articles, err := store.ListArticles(scope)
	if err != nil {
		fatal("Cannot load articles: %v", err)
	}

	// Filter to conversation articles only
	var convoArticles []*model.WikiArticle
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

	results := search.BM25(convoArticles, query, limit)
	if jsonOut {
		type result struct {
			ID       string   `json:"id"`
			Title    string   `json:"title"`
			Summary  string   `json:"summary"`
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

	articles, err := store.ListArticles(scope)
	if err != nil {
		fatal("Cannot load articles: %v", err)
	}

	var convoArticles []*model.WikiArticle
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
			ID       string   `json:"id"`
			Title    string   `json:"title"`
			Summary  string   `json:"summary"`
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
