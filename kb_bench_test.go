// kb_bench_test.go — CLI-level benchmarks (scanning the examples tree), all
// offline. The synthetic corpus generator is shared with the MCP benchmark.
// Search, parser and storage benchmarks live with their packages.
// Run: go test -bench=. -benchmem

package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// --- Benchmarks ---

func BenchmarkScanDir(b *testing.B) {
	// Small corpus (always available)
	b.Run("small_go", func(b *testing.B) {
		dir := findExamplesDir(b)
		if dir == "" {
			return
		}
		path := filepath.Join(dir, "small", "go")
		for i := 0; i < b.N; i++ {
			scanDir(path, "*.go")
		}
	})

	// Medium corpus (if downloaded)
	b.Run("medium_litestream", func(b *testing.B) {
		dir := findExamplesDir(b)
		if dir == "" {
			return
		}
		path := filepath.Join(dir, "medium", "litestream")
		if _, err := os.Stat(path); err != nil {
			b.Skip("medium corpus not downloaded — run examples/fetch.sh medium")
		}
		for i := 0; i < b.N; i++ {
			scanDir(path, "*.go")
		}
	})
}

// findExamplesDir locates the examples/ directory.
func findExamplesDir(b *testing.B) string {
	b.Helper()
	path := kbtest.Path(b, "examples")
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		b.Skip("examples/ directory not found")
	}
	return path
}

// generateCorpus creates n synthetic WikiArticles with realistic content.
func generateCorpus(n int) []*model.WikiArticle {
	rng := rand.New(rand.NewSource(42)) // deterministic
	domains := []string{"authentication", "database", "routing", "middleware", "config",
		"logging", "cache", "queue", "storage", "api", "service", "handler",
		"model", "controller", "repository", "factory", "builder", "observer"}
	adjectives := []string{"async", "distributed", "concurrent", "stateless", "encrypted",
		"cached", "batched", "streaming", "reactive", "immutable"}

	articles := make([]*model.WikiArticle, n)
	for i := 0; i < n; i++ {
		domain := domains[rng.Intn(len(domains))]
		adj := adjectives[rng.Intn(len(adjectives))]
		title := fmt.Sprintf("%s %s %d", adj, domain, i)

		// Generate realistic content (50-200 words)
		wordCount := 50 + rng.Intn(150)
		words := make([]string, wordCount)
		vocab := append(domains, adjectives...)
		vocab = append(vocab, "the", "a", "an", "is", "are", "was", "with", "for",
			"and", "or", "to", "from", "in", "on", "by", "this", "that",
			"function", "class", "method", "struct", "interface", "type",
			"returns", "handles", "processes", "manages", "creates", "deletes")
		for j := range words {
			words[j] = vocab[rng.Intn(len(vocab))]
		}

		concepts := []string{domain, adj}
		if rng.Float64() > 0.5 {
			concepts = append(concepts, domains[rng.Intn(len(domains))])
		}

		articles[i] = &model.WikiArticle{
			ID:         textutil.Slugify(title),
			Title:      title,
			Summary:    fmt.Sprintf("Article about %s %s patterns", adj, domain),
			Content:    strings.Join(words, " "),
			Concepts:   concepts,
			Categories: []string{domain},
			WordCount:  wordCount,
			Version:    1,
		}
	}
	return articles
}
