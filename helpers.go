// Small shared helpers: string and slice utilities, file checks and test-file
// filtering, scope resolution, language extensions, JSON output.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func slugify(s string) string {
	lower := strings.ToLower(s)
	re := regexp.MustCompile(`[^a-z0-9\s-]`)
	clean := re.ReplaceAllString(lower, "")
	re2 := regexp.MustCompile(`[\s-]+`)
	slug := re2.ReplaceAllString(clean, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 80 {
		slug = slug[:80]
	}
	if slug == "" {
		return contentHash(s)[:16]
	}
	return slug
}

func wordCount(s string) int {
	return len(strings.Fields(s))
}

func truncate(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func nilToEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

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

// resolveScopes handles "*" (all scopes), "a,b,c" (multi), or single scope.
func resolveScopes(scope string) []string {
	if scope == "*" {
		// List all scope directories under basePath
		entries, err := os.ReadDir(basePath())
		if err != nil {
			return nil
		}
		var scopes []string
		for _, e := range entries {
			if e.IsDir() {
				// Check it has a wiki/ dir (is a real scope)
				wikiDir := filepath.Join(basePath(), e.Name(), "wiki")
				if info, err := os.Stat(wikiDir); err == nil && info.IsDir() {
					scopes = append(scopes, e.Name())
				}
			}
		}
		return scopes
	}
	if strings.Contains(scope, ",") {
		parts := strings.Split(scope, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return parts
	}
	return []string{scope}
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

func containsStr(tokens []string, term string) bool {
	for _, t := range tokens {
		if t == term {
			return true
		}
	}
	return false
}

func countStr(tokens []string, term string) int {
	n := 0
	for _, t := range tokens {
		if t == term {
			n++
		}
	}
	return n
}

func printJSON(v any) {
	data, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(data))
}
