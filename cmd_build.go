// Implements `kb build`.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func cmdBuild(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb build <path> [--scope NAME] [--pattern GLOB] [--model MODEL]")
	}

	path := args[0]
	scope := flagStr(args, "--scope", filepath.Base(path))
	pattern := flagStr(args, "--pattern", "*.py")
	exclude := flagStr(args, "--exclude", "")
	model := flagStr(args, "--model", defaultModel)
	jsonOut := flagBool(args, "--json")
	terse := flagBool(args, "--terse")
	outputDir := flagStr(args, "--output", "")
	sinceRef := flagStr(args, "--since", "")
	// strict | loose | off (issue #19). NB: matching is on wording, not meaning —
	// paraphrased-but-agreeing sources can be flagged. See contradiction.go.
	contraMode := flagStr(args, "--contradiction-mode", "strict")
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

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

	concurrency := flagInt(args, "--concurrency", 5)

	// Phase 1: read files, check cache, collect work
	type compileJob struct {
		filePath string
		relPath  string
		text     string
		hash     string
		rawID    string
	}
	var jobs []compileJob
	var skipped int

	for _, f := range files {
		text, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: cannot read %s: %v\n", f, err)
			continue
		}

		hash := contentHash(string(text))
		relPath, _ := filepath.Rel(absPath, f)
		if relPath == "" {
			relPath = f
		}

		// --since filter: skip files not in the changed allow-list.
		// The existing hash cache still applies as an inner filter below.
		if sinceAllowList != nil && !sinceAllowList[relPath] {
			skipped++
			continue
		}

		if entry, ok := cache.Files[relPath]; ok && entry.Hash == hash {
			skipped++
			continue
		}

		jobs = append(jobs, compileJob{
			filePath: f,
			relPath:  relPath,
			text:     string(text),
			hash:     hash,
			rawID:    hash[:16],
		})
	}

	// Phase 2: compile in parallel
	type compileResult struct {
		job     compileJob
		article *WikiArticle
		usage   *TokenUsage
		fixedID bool // glossary entry with an explicit frontmatter id
	}

	var (
		mu      sync.Mutex
		results []compileResult
		wg      sync.WaitGroup
		sem     = make(chan struct{}, concurrency)
	)

	for _, job := range jobs {
		wg.Add(1)
		go func(j compileJob) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			if !jsonOut {
				fmt.Printf("Compiling: %s\n", j.relPath)
			}

			// Save raw doc
			raw := &RawDoc{
				ID:          j.rawID,
				SourceType:  "file",
				Source:      j.relPath,
				Filename:    filepath.Base(j.filePath),
				ContentType: "text",
				RawText:     j.text,
				WordCount:   wordCount(j.text),
				IngestedAt:  time.Now().UTC().Format(time.RFC3339),
			}
			saveRawDoc(scope, raw)

			var article *WikiArticle
			var usage *TokenUsage
			fixedID := false

			if isGlossarySource(j.relPath) {
				// Glossary sources are hand-curated: parse frontmatter directly,
				// preserve body verbatim, do NOT call compileLLM.
				gArt, gErr := parseGlossarySource([]byte(j.text), j.relPath)
				if gErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: glossary parse failed for %s: %v\n", j.relPath, gErr)
					article = &WikiArticle{
						ID:           slugify(strings.TrimSuffix(filepath.Base(j.filePath), filepath.Ext(j.filePath))),
						Title:        strings.TrimSuffix(filepath.Base(j.filePath), filepath.Ext(j.filePath)),
						Content:      j.text,
						Kind:         "glossary",
						WordCount:    wordCount(j.text),
						CompiledAt:   time.Now().UTC().Format(time.RFC3339),
						CompiledWith: "none (glossary fallback)",
						Version:      1,
					}
				} else {
					article = gArt
					fixedID = true
					article.Kind = "glossary"
					if article.CompiledAt == "" {
						article.CompiledAt = time.Now().UTC().Format(time.RFC3339)
					}
					if article.CompiledWith == "" {
						article.CompiledWith = "glossary (verbatim)"
					}
					if article.Version == 0 {
						article.Version = 1
					}
					if article.WordCount == 0 {
						article.WordCount = wordCount(article.Content)
					}
				}
			} else {
				// Parse AST if supported language
				codeMod := parseCode(j.filePath, j.text)

				// Compile with LLM
				compArticle, compUsage, err := compileLLM(j.text, j.relPath, model, apiKey, codeMod, terse)
				usage = compUsage
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: compilation failed for %s: %v\n", j.relPath, err)
					audience, depth, targetWords := "human", "deep", 500
					if terse {
						audience, depth, targetWords = "agent", "overview", 150
					}
					article = &WikiArticle{
						ID:           slugify(filepath.Base(j.filePath)),
						Title:        filepath.Base(j.filePath),
						Summary:      truncate(j.text, 200),
						Content:      j.text,
						WordCount:    wordCount(j.text),
						CompiledAt:   time.Now().UTC().Format(time.RFC3339),
						CompiledWith: "none (fallback)",
						Version:      1,
						Audience:     audience,
						Depth:        depth,
						TargetWords:  targetWords,
					}
				} else {
					article = compArticle
				}
			}
			article.SourcePath = j.relPath
			article.SourceDocs = []string{j.rawID}

			// Auto-tag test files
			if isTestFile(j.filePath) && !contains(article.Categories, "test") {
				article.Categories = append(article.Categories, "test")
			}

			mu.Lock()
			results = append(results, compileResult{job: j, article: article, usage: usage, fixedID: fixedID})
			mu.Unlock()
		}(job)
	}
	wg.Wait()

	// Phase 3: resolve ids, save articles, update cache (sequential for
	// consistency). Results arrive in goroutine completion order; sort them so
	// same-batch slug collisions resolve the same way every run, explicit
	// glossary ids first so compiled articles disambiguate around them.
	sort.Slice(results, func(i, j int) bool {
		if results[i].fixedID != results[j].fixedID {
			return results[i].fixedID
		}
		return results[i].job.relPath < results[j].job.relPath
	})
	ids := loadIDRegistry(scope)
	changed := len(results)
	var totalInput, totalOutput int
	for _, r := range results {
		if r.usage != nil {
			totalInput += r.usage.InputTokens
			totalOutput += r.usage.OutputTokens
		}
		if r.fixedID {
			// Keep the frontmatter version on a first write, as before.
			_, existed := ids.version[r.article.ID]
			if v := ids.claimFixed(r.article.ID, r.article.SourcePath, r.article.SourceDocs); existed {
				r.article.Version = v
			}
		} else {
			r.article.ID, r.article.Version = ids.claim(r.article.ID, r.article.SourcePath, r.article.SourceDocs, false)
		}
		saveArticle(scope, r.article)
		cache.Files[r.job.relPath] = CacheEntry{
			Hash:       r.job.hash,
			ArticleID:  r.article.ID,
			CompiledAt: r.article.CompiledAt,
		}
	}

	ids.retire(scope)
	saveCache(scope, cache)

	// Rebuild index
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	// Cross-source contradiction scan (issue #19). We feed the detector this
	// run's compiled glossary results plus the on-disk set (allArticles). The
	// case the scan catches is the common one: two glossary sources with DISTINCT
	// ids that define the same Term — both survive on disk, so allArticles carries
	// the pair and the disagreement is flagged. (The harder same-id case, where
	// two sources resolve to one wiki/<id>.md and the later write silently
	// overwrites the earlier, is NOT recovered here: detectContradictions dedupes
	// on nameKey+sourceID, and same-id candidates share a sourceID, so they
	// collapse to one member whether they arrive via results or allArticles.) The
	// per-results pass is thus redundant with allArticles for the distinct-id case
	// and a no-op for the same-id case; it is kept only as a cheap guard for a
	// future change that lets results carry a glossary article not yet on disk.
	// Within a single run `results` is append-only — no in-run duplicates to
	// recover. Detection lives in contradiction.go.
	var contradictions []Contradiction
	if contraMode != "off" {
		var buildCands []ContradictionCandidate
		for _, r := range results {
			if r.article != nil && r.article.Kind == "glossary" {
				buildCands = append(buildCands, candidatesFromArticles([]*WikiArticle{r.article})...)
			}
		}
		buildCands = append(buildCands, candidatesFromArticles(allArticles)...)
		contradictions = detectContradictions(buildCands, ContradictionConfig{Mode: contraMode})
	}

	if jsonOut {
		out := map[string]any{
			"changed":        changed,
			"cached":         skipped,
			"total":          len(files),
			"articles":       len(allArticles),
			"input_tokens":   totalInput,
			"output_tokens":  totalOutput,
			"total_tokens":   totalInput + totalOutput,
			"contradictions": contradictions,
		}
		printJSON(out)
	} else {
		fmt.Printf("\nBuilt: %d compiled, %d cached (skipped), %d total files\n", changed, skipped, len(files))
		fmt.Printf("KB: %d articles, %d concepts\n", len(allArticles), len(idx.Concepts))
		if totalInput > 0 {
			fmt.Printf("Tokens: %d input + %d output = %d total\n", totalInput, totalOutput, totalInput+totalOutput)
		}
		if len(contradictions) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d glossary contradiction(s) — sources disagree on a definition:\n", len(contradictions))
			for _, c := range contradictions {
				fmt.Fprintln(os.Stderr, "  "+formatContradictionIssue(c))
			}
			fmt.Fprintln(os.Stderr, "Resolve which definition is canonical, then rebuild. (`kb glossary validate` re-lists these.)")
		}
	}

	// Export wiki to output directory if specified
	if outputDir != "" {
		exportWiki(scope, outputDir)
	}

	// Non-zero signal so CI can gate on unresolved contradictions. Runs after
	// export so the wiki is still written. Suppressed in --json mode (the
	// "contradictions" array already carries the machine-readable signal).
	if len(contradictions) > 0 && !jsonOut {
		os.Exit(3)
	}
}
