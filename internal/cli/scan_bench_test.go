// CLI-level benchmark: scanning the examples tree (resolved from the module
// root). Search, parser and storage benchmarks live with their packages.
// Run: go test -bench=. -benchmem ./...

package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qbtrix/kb-go/internal/kbtest"
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
