// Implements `kb ingest`, plus the helpers only that command uses. Modes:
// compile one file/stdin text (built-in client or --compiler hook); --article-json
// (the caller already compiled: raw_text + article on stdin); --allow-fallback
// (explicit opt-in to store the text verbatim, also used when a compile
// fails); --vec (attach an embedding). Without a compiler and without one of
// those flags, ingest refuses with exit 2 before reading input.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdIngest(args []string) {
	scope := flagStr(args, "--scope", "default")
	source := flagStr(args, "--source", "manual")
	jsonOut := flagBool(args, "--json")
	allowFallback := flagBool(args, "--allow-fallback")
	spec := mustCompilerFromArgs(args)

	// Vector-attach mode: `kb ingest --vec <path> --id <id> --scope <s>`
	// attaches an externally-computed embedding to an existing article.
	// No LLM compilation, no raw-doc creation — pure metadata write.
	if vecPath := flagStr(args, "--vec", ""); vecPath != "" {
		articleID := flagStr(args, "--id", "")
		runIngestVec(scope, articleID, vecPath, jsonOut)
		return
	}

	// External-compile mode: `kb ingest --article-json --scope <s>` reads
	// {"raw_text": ..., "article": {...}} from stdin. The caller did the LLM
	// compilation; kb stores raw doc + article and refreshes indexes.
	if flagBool(args, "--article-json") {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("Cannot read stdin: %v", err)
		}
		if err := ingestArticleJSON(scope, data, jsonOut); err != nil {
			fatal("%v", err)
		}
		return
	}

	if !allowFallback {
		requireCompiler(spec, "ingest", "Pipe an already compiled article to `kb ingest --article-json`, or pass\n     --allow-fallback to store the text verbatim without compiling.")
	}

	ensureDirs(scope)

	var text string

	// Check for non-flag argument (file path)
	// Skip flag values: if previous arg was a flag that takes a value, skip this one
	filePath := ""
	flagsWithValues := map[string]bool{"--scope": true, "--source": true, "--lang": true, "--model": true, "--compiler": true, "--compiler-timeout": true}
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if flagsWithValues[a] {
			skipNext = true
			continue
		}
		if strings.HasPrefix(a, "--") {
			continue
		}
		filePath = a
		break
	}

	if filePath != "" {
		// Ingest from file
		data, err := os.ReadFile(filePath)
		if err != nil {
			fatal("Cannot read file: %v", err)
		}
		text = string(data)
		if source == "manual" {
			source = filePath
		}
	} else {
		// Read from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fatal("Cannot read stdin: %v", err)
		}
		text = string(data)
	}

	lang := flagStr(args, "--lang", "")
	if err := ingestText(scope, source, spec, lang, filePath, text, allowFallback, jsonOut); err != nil {
		fatal("%v", err)
	}
}

// ingestText saves the raw doc, compiles it (hook or built-in client), and
// refreshes the indexes. With allowFallback the text is stored verbatim
// (CompiledWith "none (fallback)") when there is no compiler or the compile
// fails; without it a failed compile returns an error — the raw doc is
// already persisted at that point, so no content is lost, but NO article is
// written. Split out of cmdIngest so the failure contract is testable
// in-process (fatal calls os.Exit).
func ingestText(scope, source string, spec compilerSpec, lang, filePath, text string, allowFallback, jsonOut bool) error {
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("No content to ingest")
	}
	if !spec.enabled() && !allowFallback {
		return fmt.Errorf("no compiler configured: set ANTHROPIC_API_KEY, or pass --compiler, --article-json or --allow-fallback")
	}

	ensureDirs(scope)

	// Save raw doc
	hash := contentHash(text)
	raw := &RawDoc{
		ID:          hash[:16],
		SourceType:  "text",
		Source:      source,
		Filename:    filepath.Base(source),
		ContentType: "text",
		RawText:     text,
		WordCount:   wordCount(text),
		IngestedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	if err := saveRawDoc(scope, raw); err != nil {
		return fmt.Errorf("failed to save raw doc: %v", err)
	}

	// Parse AST if it's a code file
	var codeMod *CodeModule
	if filePath != "" {
		codeMod = parseCode(filePath, text)
	} else if lang != "" {
		// Use --lang flag for stdin input (e.g., --lang go)
		fakeFile := "stdin." + langToExt(lang)
		codeMod = parseCode(fakeFile, text)
	}

	// Compile — ingest always uses non-terse mode (full documentation).
	var article *WikiArticle
	err := fmt.Errorf("no compiler configured")
	if spec.enabled() {
		article, err = compileArticle(spec, text, source, codeMod, false)
	}
	if err != nil {
		if !allowFallback {
			// Loud fail: a verbatim fallback article poisons the scope (raw
			// dumps rank in search and bloat every BM25 pass). Keep the raw
			// doc, write no article, tell the caller how to proceed.
			return fmt.Errorf("compile failed: %v\nRaw doc %s is saved in scope %q — no article was written.\nRe-run with --allow-fallback to store the raw text verbatim as an article, or use --article-json to supply an externally compiled article.", err, raw.ID, scope)
		}
		article = &WikiArticle{
			ID:           slugify(filepath.Base(source)),
			Title:        filepath.Base(source),
			Summary:      truncate(text, 200),
			Content:      text,
			WordCount:    wordCount(text),
			CompiledAt:   time.Now().UTC().Format(time.RFC3339),
			CompiledWith: "none (fallback)",
			Version:      1,
		}
	}
	article.SourcePath = source
	article.SourceDocs = []string{raw.ID}

	ids := loadIDRegistry(scope)
	article.ID, article.Version = ids.claim(article.ID, article.SourcePath, article.SourceDocs, true)
	if err := saveArticle(scope, article); err != nil {
		return fmt.Errorf("failed to save article %s: %v", article.ID, err)
	}
	ids.retire(scope)
	finishIngest(scope, article, jsonOut)
	return nil
}

// ingestArticleJSON implements `kb ingest --article-json`: the stdin payload
// carries both the raw text and the already-compiled article, so external
// callers (e.g. PocketPaw's own LLM backend) can populate a scope with their
// own model. An optional article.usage is stored as the article's spend
// record. Deliberately a separate single-doc contract from
// cmdAccept: accept assumes prepare already wrote the raw docs and is a
// multi-article batch keyed by source hash; this mode owns raw-doc creation
// and linking for one doc.
func ingestArticleJSON(scope string, data []byte, jsonOut bool) error {
	var payload struct {
		RawText string `json:"raw_text"`
		Article struct {
			Title        string          `json:"title"`
			Summary      string          `json:"summary"`
			Content      string          `json:"content"`
			Concepts     []string        `json:"concepts"`
			Categories   []string        `json:"categories"`
			Source       string          `json:"source"`
			CompiledWith string          `json:"compiled_with"`
			Usage        json.RawMessage `json:"usage"`
		} `json:"article"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("--article-json: invalid JSON on stdin: %v", err)
	}
	if strings.TrimSpace(payload.RawText) == "" {
		return fmt.Errorf("--article-json: raw_text is required")
	}
	if strings.TrimSpace(payload.Article.Title) == "" {
		return fmt.Errorf("--article-json: article.title is required")
	}
	if strings.TrimSpace(payload.Article.Content) == "" {
		return fmt.Errorf("--article-json: article.content is required")
	}

	ensureDirs(scope)

	source := payload.Article.Source
	if source == "" {
		source = "external"
	}
	now := time.Now().UTC().Format(time.RFC3339)

	hash := contentHash(payload.RawText)
	raw := &RawDoc{
		ID:          hash[:16],
		SourceType:  "text",
		Source:      source,
		Filename:    filepath.Base(source),
		ContentType: "text",
		RawText:     payload.RawText,
		WordCount:   wordCount(payload.RawText),
		IngestedAt:  now,
	}
	if err := saveRawDoc(scope, raw); err != nil {
		return fmt.Errorf("failed to save raw doc: %v", err)
	}

	usage := parseUsage(payload.Article.Usage)
	compiledWith := compiledWithFor(payload.Article.CompiledWith, usage, "external")

	article := &WikiArticle{
		ID:           slugify(payload.Article.Title),
		Title:        payload.Article.Title,
		Summary:      payload.Article.Summary,
		Content:      payload.Article.Content,
		Concepts:     nilToEmpty(payload.Article.Concepts),
		Categories:   nilToEmpty(payload.Article.Categories),
		SourcePath:   payload.Article.Source,
		SourceDocs:   []string{raw.ID},
		WordCount:    wordCount(payload.Article.Content),
		CompiledAt:   now,
		CompiledWith: compiledWith,
		Version:      1,
		Audience:     "human",
		Depth:        "deep",
		TargetWords:  500,
		Usage:        usage,
	}

	ids := loadIDRegistry(scope)
	article.ID, article.Version = ids.claim(article.ID, article.SourcePath, article.SourceDocs, true)
	if err := saveArticle(scope, article); err != nil {
		return fmt.Errorf("failed to save article %s: %v", article.ID, err)
	}
	ids.retire(scope)
	finishIngest(scope, article, jsonOut)
	return nil
}

// finishIngest refreshes the concept index + search index after an ingest
// write and prints the standard ingest output (shared by both ingest modes).
func finishIngest(scope string, article *WikiArticle, jsonOut bool) {
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	if jsonOut {
		printJSON(map[string]any{
			"article":       article.ID,
			"title":         article.Title,
			"words":         article.WordCount,
			"compiled_with": article.CompiledWith,
		})
	} else {
		fmt.Printf("Ingested: %s (%d words)\n", article.Title, article.WordCount)
		if len(article.Concepts) > 0 {
			fmt.Printf("  Concepts: %s\n", strings.Join(article.Concepts, ", "))
		}
	}
}
