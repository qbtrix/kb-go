// Small CLI helpers: slice pruning, file checks, build file filtering
// (exclude patterns, test-file detection), language extensions and JSON output.

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// removeString returns a copy of slice with every occurrence of item dropped.
// Used by cmdDelete to prune an article id from a concept's article list.
func removeString(slice []string, item string) []string {
	out := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			out = append(out, s)
		}
	}
	return out
}

// fileExists reports whether path exists and is readable (best-effort — a stat
// error other than not-exist is treated as absent). Used by cmdDelete to decide
// whether an article's on-disk files were present before removal.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// excludeFiles removes files matching comma-separated exclude patterns.
func excludeFiles(files []string, excludePattern string) []string {
	patterns := strings.Split(excludePattern, ",")
	for i := range patterns {
		patterns[i] = strings.TrimSpace(patterns[i])
	}
	var result []string
	for _, f := range files {
		excluded := false
		for _, p := range patterns {
			// Match against basename (e.g. "*.test.*")
			if matched, _ := filepath.Match(p, filepath.Base(f)); matched {
				excluded = true
				break
			}
			// Also match against full path for directory patterns (e.g. "*components/ui*")
			if strings.Contains(f, strings.Trim(p, "*")) && strings.Contains(p, "/") {
				excluded = true
				break
			}
		}
		if !excluded {
			result = append(result, f)
		}
	}
	return result
}

// isTestFile detects test files by common naming patterns.
func isTestFile(path string) bool {
	base := filepath.Base(path)
	lower := strings.ToLower(base)
	testPatterns := []string{
		".test.", ".spec.", "_test.", "_spec.",
		"test_", "spec_",
	}
	for _, p := range testPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	// Check if inside a __tests__ or test/ directory
	dir := strings.ToLower(filepath.Dir(path))
	return strings.Contains(dir, "__tests__") || strings.Contains(dir, "/test/") || strings.Contains(dir, "/tests/")
}

func langToExt(lang string) string {
	switch strings.ToLower(lang) {
	case "go", "golang":
		return "go"
	case "python", "py":
		return "py"
	case "typescript", "ts":
		return "ts"
	case "javascript", "js":
		return "js"
	default:
		return lang
	}
}

func printJSON(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
