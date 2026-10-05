// kb_bench_test.go — Performance benchmarks for kb-go, all offline. Includes
// BenchmarkSearchLargeCorpus (50 docs x 50k words), which compares the
// inverted-index fast path with the tokenize-on-the-fly slow path. Example
// files resolve from the module root (kbtest.Path).
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
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// --- Helpers ---

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

// --- Benchmarks ---

func BenchmarkTokenize(b *testing.B) {
	sizes := map[string]int{"100w": 100, "1Kw": 1000, "10Kw": 10000}
	for name, count := range sizes {
		words := make([]string, count)
		for i := range words {
			words[i] = "benchmark"
		}
		text := strings.Join(words, " test data for ")

		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				tokenize(text)
			}
			b.ReportMetric(float64(count)/float64(b.Elapsed().Seconds())*float64(b.N)/float64(b.N), "words/sec")
		})
	}
}

// generateLargeCorpus builds nDocs articles of wordsPerDoc words each — the
// shape of a scope poisoned by verbatim raw-dump ingests (few articles, huge
// bodies). Deterministic via a fixed seed.
func generateLargeCorpus(nDocs, wordsPerDoc int) []*model.WikiArticle {
	rng := rand.New(rand.NewSource(7))
	vocab := []string{"authentication", "database", "routing", "middleware", "config",
		"logging", "cache", "queue", "storage", "api", "service", "handler",
		"the", "a", "is", "with", "for", "and", "to", "from", "in", "on",
		"function", "returns", "handles", "processes", "manages", "creates",
		"session", "token", "request", "response", "error", "retry", "timeout"}
	articles := make([]*model.WikiArticle, nDocs)
	for i := 0; i < nDocs; i++ {
		words := make([]string, wordsPerDoc)
		for j := range words {
			words[j] = vocab[rng.Intn(len(vocab))]
		}
		title := fmt.Sprintf("raw dump %d", i)
		articles[i] = &model.WikiArticle{
			ID:         textutil.Slugify(title),
			Title:      title,
			Summary:    "verbatim raw text",
			Content:    strings.Join(words, " "),
			Concepts:   []string{vocab[i%12]},
			Categories: []string{"raw"},
			WordCount:  wordsPerDoc,
			Version:    1,
		}
	}
	return articles
}

// BenchmarkSearchLargeCorpus measures one search over a 50-doc x 50k-word
// corpus (2.5M words — the poisoned-scope shape) with the inverted index vs
// the tokenize-on-the-fly slow path.
func BenchmarkSearchLargeCorpus(b *testing.B) {
	corpus := generateLargeCorpus(50, 50000)
	query := "authentication session timeout"

	si := buildSearchIndex(corpus)
	b.Run("inverted_index", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bm25SearchWithIndex(corpus, query, 5, si)
		}
	})
	b.Run("slow_path", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			bm25SearchWithIndex(corpus, query, 5, nil)
		}
	})
}

func BenchmarkBM25Search(b *testing.B) {
	for _, size := range []int{10, 100, 1000, 5000} {
		corpus := generateCorpus(size)
		b.Run(fmt.Sprintf("corpus_%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				bm25Search(corpus, "authentication service handler middleware", 5)
			}
		})
	}
}

func BenchmarkRebuildIndex(b *testing.B) {
	for _, size := range []int{10, 100, 1000} {
		corpus := generateCorpus(size)
		b.Run(fmt.Sprintf("articles_%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				store.RebuildIndex("bench", corpus)
			}
		})
	}
}

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
