// Implements `kb prepare`.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/parse"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// cmdPrepare scans files and outputs compilation prompts as JSON.
// Used in agent mode — the calling agent (Claude Code, Cursor, etc.)
// processes each prompt using its own LLM, then pipes results to `kb accept`.
func cmdPrepare(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb prepare <path> [--scope NAME] [--pattern GLOB] [--exclude GLOB]")
	}

	path := args[0]
	scope := flagStr(args, "--scope", filepath.Base(path))
	pattern := flagStr(args, "--pattern", "*.py")
	exclude := flagStr(args, "--exclude", "")
	terse := flagBool(args, "--terse")
	sinceRef := flagStr(args, "--since", "")

	absPath, err := filepath.Abs(path)
	if err != nil {
		fatal("Invalid path: %s", path)
	}

	ensureDirs(scope)
	cache := loadCache(scope)

	files := scanDir(absPath, pattern)
	if exclude != "" {
		files = excludeFiles(files, exclude)
	}
	if len(files) == 0 {
		fatal("No files found matching %s in %s", pattern, absPath)
	}

	// Compute --since allow-list before the scan loop.
	// On any failure, warn and fall back to a full build (allow-list = nil).
	var sinceAllowList map[string]bool
	if sinceRef != "" {
		list, err := changedFilesSinceRef(absPath, sinceRef)
		if err != nil {
			fmt.Fprintf(os.Stderr, "--since: git lookup failed (%v); falling back to full build\n", err)
		} else {
			sinceAllowList = list
		}
	}

	type prepareItem struct {
		Source  string `json:"source"`
		Hash    string `json:"hash"`
		RawID   string `json:"raw_id"`
		Prompt  string `json:"prompt"`
		IsTest  bool   `json:"is_test"`
		IsTerse bool   `json:"is_terse"`
	}

	var items []prepareItem
	var skipped int

	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot read %s: %v\n", f, err)
			continue
		}

		hash := textutil.ContentHash(string(text))
		relPath, _ := filepath.Rel(absPath, f)
		if relPath == "" {
			relPath = f
		}

		// --since filter: skip files not in the changed allow-list.
		if sinceAllowList != nil && !sinceAllowList[relPath] {
			skipped++
			continue
		}

		if entry, ok := cache.Files[relPath]; ok && entry.Hash == hash {
			skipped++
			continue
		}

		// Save raw doc
		rawID := hash[:16]
		raw := &model.RawDoc{
			ID:          rawID,
			SourceType:  "file",
			Source:      relPath,
			Filename:    filepath.Base(f),
			ContentType: "text",
			RawText:     string(text),
			WordCount:   textutil.WordCount(string(text)),
			IngestedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		saveRawDoc(scope, raw)

		// Build the same prompt `kb build` sends — shared helpers keep them in sync.
		prompt := compile.Prompt(relPath, parse.PromptBlock(parse.Code(f, string(text))), string(text), terse)

		items = append(items, prepareItem{
			Source:  relPath,
			Hash:    hash,
			RawID:   rawID,
			Prompt:  prompt,
			IsTest:  isTestFile(f),
			IsTerse: terse,
		})
	}

	output := map[string]any{
		"scope":   scope,
		"items":   items,
		"pending": len(items),
		"cached":  skipped,
		"total":   len(files),
	}
	printJSON(output)
}
