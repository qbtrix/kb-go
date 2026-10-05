// Package model holds the data types every kb package shares: raw docs, wiki
// articles and their optional compile usage, the knowledge index and its
// concepts, build-cache entries and lint issues. Field names and JSON tags
// mirror the Python models.py, so both implementations read the same files.
// Plain data only: no I/O and no internal imports.
package model

type RawDoc struct {
	ID          string            `json:"id"`
	SourceType  string            `json:"source_type"`
	Source      string            `json:"source"`
	Filename    string            `json:"filename,omitempty"`
	ContentType string            `json:"content_type"`
	RawText     string            `json:"raw_text"`
	WordCount   int               `json:"word_count"`
	IngestedAt  string            `json:"ingested_at"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

type WikiArticle struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Content    string   `json:"-"`
	Concepts   []string `json:"concepts"`
	Categories []string `json:"categories"`
	// SourcePath is the human-readable relative path of the source file this
	// article was compiled from (e.g. "src/auth/login.go"). Optional for
	// backward compatibility — legacy articles load with an empty string.
	// Consumers (index builders, UIs) should prefer SourcePath over SourceDocs
	// when grouping by code structure.
	SourcePath   string   `json:"source_path,omitempty"`
	SourceDocs   []string `json:"source_docs"`
	Backlinks    []string `json:"backlinks"`
	WordCount    int      `json:"word_count"`
	CompiledAt   string   `json:"compiled_at"`
	CompiledWith string   `json:"compiled_with"`
	Version      int      `json:"version"`
	// Terse-mode metadata: audience (agent|human), depth (overview|deep), target_words.
	// Defaults when loading legacy articles: audience=human, depth=deep, target_words=500.
	Audience    string `json:"audience,omitempty"`
	Depth       string `json:"depth,omitempty"`
	TargetWords int    `json:"target_words,omitempty"`
	// Glossary metadata (issue #15). Kind == "" is the default module article;
	// Kind == "glossary" marks a hand-curated definition that round-trips through
	// the wiki without LLM rewriting. All optional for backward compatibility.
	Kind     string   `json:"kind,omitempty"`     // "" = module article (default), "glossary" = hand-curated
	Term     string   `json:"term,omitempty"`     // glossary: canonical term
	Aliases  []string `json:"aliases,omitempty"`  // glossary: alternative names
	Category string   `json:"category,omitempty"` // glossary: single category (distinct from Categories []string)
	Related  []string `json:"related,omitempty"`  // glossary: refs to other terms/aliases
	// Usage is the optional spend record reported by whoever compiled the
	// article (compiler hook, accept, ingest --article-json). nil on articles
	// that never reported one, including every pre-v0.4 article.
	Usage *ArticleUsage `json:"usage,omitempty"`
}

// Frontmatter is the JSON block at the top of .md files.
type Frontmatter struct {
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Concepts     []string `json:"concepts"`
	Categories   []string `json:"categories"`
	SourcePath   string   `json:"source_path,omitempty"`
	SourceDocs   []string `json:"source_docs"`
	Backlinks    []string `json:"backlinks"`
	WordCount    int      `json:"word_count"`
	CompiledAt   string   `json:"compiled_at"`
	CompiledWith string   `json:"compiled_with"`
	Version      int      `json:"version"`
	// Terse-mode metadata — mirrors WikiArticle fields.
	Audience    string `json:"audience,omitempty"`
	Depth       string `json:"depth,omitempty"`
	TargetWords int    `json:"target_words,omitempty"`
	// Glossary metadata — mirrors WikiArticle fields (issue #15).
	Kind     string   `json:"kind,omitempty"`
	Term     string   `json:"term,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	Category string   `json:"category,omitempty"`
	Related  []string `json:"related,omitempty"`
	// Compile spend: mirrors WikiArticle.Usage; omitted when absent.
	Usage *ArticleUsage `json:"usage,omitempty"`
}

type Concept struct {
	Name     string   `json:"name"`
	Articles []string `json:"articles"`
	Category string   `json:"category,omitempty"`
}

type KnowledgeIndex struct {
	Scope      string              `json:"scope"`
	Articles   map[string]any      `json:"articles"`
	Concepts   map[string]*Concept `json:"concepts"`
	Categories []string            `json:"categories"`
}

type CacheEntry struct {
	Hash       string `json:"hash"`
	ArticleID  string `json:"article_id"`
	CompiledAt string `json:"compiled_at"`
}

type Cache struct {
	Version int                   `json:"version"`
	Files   map[string]CacheEntry `json:"files"`
}

type LintIssue struct {
	Type       string `json:"type"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	ArticleID  string `json:"article_id,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ArticleUsage is the optional spend record a compiler (or an accept /
// --article-json caller) reports for one article. Stored in the article
// frontmatter as "usage"; replaced, never summed, when the article is
// recompiled or re-accepted.
type ArticleUsage struct {
	Model        string  `json:"model,omitempty"`
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}
