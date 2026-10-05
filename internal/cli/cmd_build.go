// Implements `kb build`: scan, hash-cache skip, compile pending files through
// the configured compile path (--compiler / KB_COMPILER, else the built-in
// Anthropic client; glossary files pass through verbatim, never compiled),
// save articles, rebuild indexes. A file whose compile fails gets no article
// and no cache entry (the next build retries it) and makes the command exit
// 1; files that compiled are still saved. With no compile path configured,
// build refuses (exit 2) when anything needs compiling.

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qbtrix/kb-go/internal/compile"
	"github.com/qbtrix/kb-go/internal/contradiction"
	"github.com/qbtrix/kb-go/internal/export"
	"github.com/qbtrix/kb-go/internal/glossary"
	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/parse"
	"github.com/qbtrix/kb-go/internal/search"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

func cmdBuild(args []string) {
	if code := runBuild(args); code != 0 {
		os.Exit(code)
	}
}

// runBuild is cmdBuild returning its exit code, so `kb watch` can rebuild
// after a failed compile without exiting.
func runBuild(args []string) int {
	if len(args) < 1 {
		fatal("Usage: kb build <path> [--scope NAME] [--pattern GLOB] [--model MODEL | --compiler \"<command>\"]")
	}

	path := args[0]
	scope := flagStr(args, "--scope", filepath.Base(path))
	pattern := flagStr(args, "--pattern", "*.py")
	exclude := flagStr(args, "--exclude", "")
	jsonOut := flagBool(args, "--json")
	terse := flagBool(args, "--terse")
	outputDir := flagStr(args, "--output", "")
	sinceRef := flagStr(args, "--since", "")
	// strict | loose | off (issue #19). NB: matching is on wording, not meaning —
	// paraphrased-but-agreeing sources can be flagged. See internal/contradiction.
	contraMode := flagStr(args, "--contradiction-mode", "strict")
	spec := mustCompilerFromArgs(args)

	absPath, err := filepath.Abs(path)
	if err != nil {
		fatal("Invalid path: %s", path)
	}

	store.EnsureDirs(scope)
	cache := store.LoadCache(scope)

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

		hash := textutil.ContentHash(string(text))
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

	// Refuse before writing anything when a file needs compiling and there is
	// no compiler. Glossary-only and fully cached builds need none.
	for _, j := range jobs {
		if !glossary.IsSource(j.relPath) {
			requireCompiler(spec, "build", "Compile in your own agent: `kb prepare` emits the prompts, `kb accept` stores the articles.")
			break
		}
	}

	// Phase 2: compile in parallel
	type compileResult struct {
		job     compileJob
		article *model.WikiArticle
		fixedID bool // glossary entry with an explicit frontmatter id
	}

	var (
		mu      sync.Mutex
		results []compileResult
		failed  []string
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
			raw := &model.RawDoc{
				ID:          j.rawID,
				SourceType:  "file",
				Source:      j.relPath,
				Filename:    filepath.Base(j.filePath),
				ContentType: "text",
				RawText:     j.text,
				WordCount:   textutil.WordCount(j.text),
				IngestedAt:  time.Now().UTC().Format(time.RFC3339),
			}
			store.SaveRawDoc(scope, raw)

			var article *model.WikiArticle
			fixedID := false

			if glossary.IsSource(j.relPath) {
				// Glossary sources are hand-curated: parse frontmatter directly,
				// preserve body verbatim, never sent to the compiler.
				gArt, gErr := glossary.ParseSource([]byte(j.text), j.relPath)
				if gErr != nil {
					fmt.Fprintf(os.Stderr, "Warning: glossary parse failed for %s: %v\n", j.relPath, gErr)
					article = &model.WikiArticle{
						ID:           textutil.Slugify(strings.TrimSuffix(filepath.Base(j.filePath), filepath.Ext(j.filePath))),
						Title:        strings.TrimSuffix(filepath.Base(j.filePath), filepath.Ext(j.filePath)),
						Content:      j.text,
						Kind:         "glossary",
						WordCount:    textutil.WordCount(j.text),
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
						article.WordCount = textutil.WordCount(article.Content)
					}
				}
			} else {
				// Parse AST if supported language
				codeMod := parse.Code(j.filePath, j.text)

				// Compile (hook or built-in client). A failure writes no
				// article and no cache entry: never the raw text in its place.
				compArticle, err := compile.Article(spec, j.text, j.relPath, parse.PromptBlock(codeMod), terse)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error: compile failed for %s: %v\n", j.relPath, err)
					mu.Lock()
					failed = append(failed, j.relPath)
					mu.Unlock()
					return
				}
				article = compArticle
			}
			article.SourcePath = j.relPath
			article.SourceDocs = []string{j.rawID}

			// Auto-tag test files
			if isTestFile(j.filePath) && !slices.Contains(article.Categories, "test") {
				article.Categories = append(article.Categories, "test")
			}

			mu.Lock()
			results = append(results, compileResult{job: j, article: article, fixedID: fixedID})
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
	ids := store.LoadIDRegistry(scope)
	changed := len(results)
	var totalInput, totalOutput int
	for _, r := range results {
		if r.article.Usage != nil {
			totalInput += r.article.Usage.InputTokens
			totalOutput += r.article.Usage.OutputTokens
		}
		if r.fixedID {
			// Keep the frontmatter version on a first write, as before.
			existed := ids.Has(r.article.ID)
			if v := ids.ClaimFixed(r.article.ID, r.article.SourcePath, r.article.SourceDocs); existed {
				r.article.Version = v
			}
		} else {
			r.article.ID, r.article.Version = ids.Claim(r.article.ID, r.article.SourcePath, r.article.SourceDocs, false)
		}
		store.SaveArticle(scope, r.article)
		cache.Files[r.job.relPath] = model.CacheEntry{
			Hash:       r.job.hash,
			ArticleID:  r.article.ID,
			CompiledAt: r.article.CompiledAt,
		}
	}

	ids.Retire(scope)
	store.SaveCache(scope, cache)

	// Rebuild index
	allArticles, _ := store.ListArticles(scope)
	idx := store.RebuildIndex(scope, allArticles)
	store.SaveIndex(scope, idx)
	search.SaveIndex(scope, search.BuildIndex(allArticles))

	// Cross-source contradiction scan (issue #19). We feed the detector this
	// run's compiled glossary results plus the on-disk set (allArticles). The
	// case the scan catches is the common one: two glossary sources with DISTINCT
	// ids that define the same Term — both survive on disk, so allArticles carries
	// the pair and the disagreement is flagged. (The harder same-id case, where
	// two sources resolve to one wiki/<id>.md and the later write silently
	// overwrites the earlier, is NOT recovered here: contradiction.Detect dedupes
	// on nameKey+sourceID, and same-id candidates share a sourceID, so they
	// collapse to one member whether they arrive via results or allArticles.) The
	// per-results pass is thus redundant with allArticles for the distinct-id case
	// and a no-op for the same-id case; it is kept only as a cheap guard for a
	// future change that lets results carry a glossary article not yet on disk.
	// Within a single run `results` is append-only — no in-run duplicates to
	// recover. Detection lives in internal/contradiction.
	var contradictions []contradiction.Finding
	if contraMode != "off" {
		var buildCands []contradiction.Candidate
		for _, r := range results {
			if r.article != nil && r.article.Kind == "glossary" {
				buildCands = append(buildCands, contradiction.CandidatesFromArticles([]*model.WikiArticle{r.article})...)
			}
		}
		buildCands = append(buildCands, contradiction.CandidatesFromArticles(allArticles)...)
		contradictions = contradiction.Detect(buildCands, contradiction.Config{Mode: contraMode})
	}

	sort.Strings(failed)
	if jsonOut {
		out := map[string]any{
			"changed":        changed,
			"failed":         len(failed),
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
		if len(failed) > 0 {
			fmt.Printf("Failed: %d (no article written; the next build retries them)\n", len(failed))
		}
		fmt.Printf("KB: %d articles, %d concepts\n", len(allArticles), len(idx.Concepts))
		if totalInput > 0 {
			fmt.Printf("Tokens: %d input + %d output = %d total\n", totalInput, totalOutput, totalInput+totalOutput)
		}
		if len(contradictions) > 0 {
			fmt.Fprintf(os.Stderr, "\n%d glossary contradiction(s) — sources disagree on a definition:\n", len(contradictions))
			for _, c := range contradictions {
				fmt.Fprintln(os.Stderr, "  "+contradiction.FormatIssue(c))
			}
			fmt.Fprintln(os.Stderr, "Resolve which definition is canonical, then rebuild. (`kb glossary validate` re-lists these.)")
		}
	}

	// Export wiki to output directory if specified
	if outputDir != "" {
		export.Wiki(scope, outputDir)
	}

	// Failed compiles exit 1 in every output mode (after the successes and the
	// export are written).
	if len(failed) > 0 {
		fmt.Fprintf(os.Stderr, "%d file(s) failed to compile: %s\n", len(failed), strings.Join(failed, ", "))
		return 1
	}

	// Non-zero signal so CI can gate on unresolved contradictions. Runs after
	// export so the wiki is still written. Suppressed in --json mode (the
	// "contradictions" array already carries the machine-readable signal).
	if len(contradictions) > 0 && !jsonOut {
		return 3
	}
	return 0
}
