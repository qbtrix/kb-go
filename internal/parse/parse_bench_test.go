// Parser benchmarks over the example sources in examples/small (resolved from
// the module root). Run: go test -bench=. -benchmem ./internal/parse

package parse

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
)

// loadExampleFile reads a file from examples/ relative to the module root.
func loadExampleFile(b *testing.B, relPath string) string {
	b.Helper()
	data, err := os.ReadFile(filepath.Join(kbtest.Path(b, "examples"), relPath))
	if err != nil {
		b.Skipf("example file not found: examples/%s", relPath)
	}
	return string(data)
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

func BenchmarkFormatCodeContext(b *testing.B) {
	source := loadExampleFile(b, "small/go/server.go")
	mod := parseGo("server.go", source)
	if mod == nil {
		b.Skip("failed to parse Go file")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		FormatContext(mod)
	}
}
