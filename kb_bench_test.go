// kb_bench_test.go — Performance benchmarks for kb-go.
// All offline, no API key needed. Run: go test -bench=. -benchmem
package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Helpers ---

// generateCorpus creates n synthetic WikiArticles with realistic content.
func generateCorpus(n int) []*WikiArticle {
	rng := rand.New(rand.NewSource(42)) // deterministic
	domains := []string{"authentication", "database", "routing", "middleware", "config",
		"logging", "cache", "queue", "storage", "api", "service", "handler",
		"model", "controller", "repository", "factory", "builder", "observer"}
	adjectives := []string{"async", "distributed", "concurrent", "stateless", "encrypted",
		"cached", "batched", "streaming", "reactive", "immutable"}

	articles := make([]*WikiArticle, n)
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

		articles[i] = &WikiArticle{
			ID:         slugify(title),
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

// loadExampleFile reads a file from examples/ relative to the module root.
func loadExampleFile(b *testing.B, relPath string) string {
	b.Helper()
	// Try relative to working dir, then up one level
	for _, base := range []string{".", ".."} {
		path := filepath.Join(base, "examples", relPath)
		data, err := os.ReadFile(path)
		if err == nil {
			return string(data)
		}
	}
	b.Skipf("example file not found: examples/%s", relPath)
	return ""
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

func BenchmarkContentHash(b *testing.B) {
	sizes := map[string]int{"1KB": 1024, "10KB": 10240, "100KB": 102400}
	for name, size := range sizes {
		data := strings.Repeat("x", size)
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				contentHash(data)
			}
		})
	}
}

func BenchmarkSlugify(b *testing.B) {
	inputs := []string{
		"Simple Title",
		"GroupService — manages group operations and membership",
		"A Very Long Title That Should Be Truncated Because It Exceeds The Maximum Length Allowed",
		"special!@#$%^&*()chars",
	}
	for _, input := range inputs {
		b.Run(fmt.Sprintf("len_%d", len(input)), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				slugify(input)
			}
		})
	}
}

func BenchmarkParseGo(b *testing.B) {
	files := []string{
		"small/go/server.go",
		"small/go/handler.go",
		"small/go/middleware.go",
		"small/go/models.go",
		"small/go/config.go",
	}

	// Single file
	source := loadExampleFile(b, files[0])
	b.Run("single_file", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			parseGo("server.go", source)
		}
	})

	// All files
	sources := make([]struct{ name, src string }, 0, len(files))
	for _, f := range files {
		src := loadExampleFile(b, f)
		sources = append(sources, struct{ name, src string }{filepath.Base(f), src})
	}
	b.Run("all_5_files", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, s := range sources {
				parseGo(s.name, s.src)
			}
		}
		b.ReportMetric(float64(len(sources)*b.N)/b.Elapsed().Seconds(), "files/sec")
	})
}

func BenchmarkParsePython(b *testing.B) {
	files := []string{
		"small/python/service.py",
		"small/python/models.py",
		"small/python/utils.py",
	}

	source := loadExampleFile(b, files[0])
	b.Run("single_file", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			parsePython("service.py", source)
		}
	})

	sources := make([]struct{ name, src string }, 0, len(files))
	for _, f := range files {
		src := loadExampleFile(b, f)
		sources = append(sources, struct{ name, src string }{filepath.Base(f), src})
	}
	b.Run("all_3_files", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, s := range sources {
				parsePython(s.name, s.src)
			}
		}
		b.ReportMetric(float64(len(sources)*b.N)/b.Elapsed().Seconds(), "files/sec")
	})
}

func BenchmarkParseTypeScript(b *testing.B) {
	files := []string{
		"small/typescript/api.ts",
		"small/typescript/types.ts",
	}

	source := loadExampleFile(b, files[0])
	b.Run("single_file", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			parseTypeScript("api.ts", source, "typescript")
		}
	})

	sources := make([]struct{ name, src string }, 0, len(files))
	for _, f := range files {
		src := loadExampleFile(b, f)
		sources = append(sources, struct{ name, src string }{filepath.Base(f), src})
	}
	b.Run("all_2_files", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			for _, s := range sources {
				parseTypeScript(s.name, s.src, "typescript")
			}
		}
		b.ReportMetric(float64(len(sources)*b.N)/b.Elapsed().Seconds(), "files/sec")
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
				rebuildIndex("bench", corpus)
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

func BenchmarkFormatCodeContext(b *testing.B) {
	source := loadExampleFile(b, "small/go/server.go")
	mod := parseGo("server.go", source)
	if mod == nil {
		b.Skip("failed to parse Go file")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatCodeContext(mod)
	}
}

// findExamplesDir locates the examples/ directory.
func findExamplesDir(b *testing.B) string {
	b.Helper()
	for _, base := range []string{".", ".."} {
		path := filepath.Join(base, "examples")
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			abs, _ := filepath.Abs(path)
			return abs
		}
	}
	b.Skip("examples/ directory not found")
	return ""
}
