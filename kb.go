// kb.go — Single-file knowledge base engine.
// Headless, lean CLI. BM25 search over LLM-compiled articles.
// No embeddings, no vectors. The LLM understands at write time, not query time.
//
// Changes: Added agent mode (prepare/accept) — lets calling agents like Claude Code,
// Cursor, or Codex compile articles using their own LLM. No API key needed.
//
// Commands: build, prepare, accept, search, ingest, show, list, stats, lint, recompile, watch, clear
// AST parsing: Go (stdlib go/ast), Python (regex), TypeScript/JS (regex)
// Storage: markdown + JSON frontmatter (compatible with Python knowledge-base package)
// External deps: fsnotify (watch mode only)
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/fsnotify/fsnotify"
)

const (
	defaultModel   = "claude-haiku-4-5-20251001"
	defaultBaseDir = ".knowledge-base"
	apiURL         = "https://api.anthropic.com/v1/messages"
	apiVersion     = "2023-06-01"
	bm25K1         = 1.2
	bm25B          = 0.75
)

// --- Data Models (mirrors Python models.py) ---

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
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Content      string   `json:"-"`
	Concepts     []string `json:"concepts"`
	Categories   []string `json:"categories"`
	SourceDocs   []string `json:"source_docs"`
	Backlinks    []string `json:"backlinks"`
	WordCount    int      `json:"word_count"`
	CompiledAt   string   `json:"compiled_at"`
	CompiledWith string   `json:"compiled_with"`
	Version      int      `json:"version"`
}

// Frontmatter is the JSON block at the top of .md files.
type Frontmatter struct {
	Title        string   `json:"title"`
	Summary      string   `json:"summary"`
	Concepts     []string `json:"concepts"`
	Categories   []string `json:"categories"`
	SourceDocs   []string `json:"source_docs"`
	Backlinks    []string `json:"backlinks"`
	WordCount    int      `json:"word_count"`
	CompiledAt   string   `json:"compiled_at"`
	CompiledWith string   `json:"compiled_with"`
	Version      int      `json:"version"`
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
	Type      string `json:"type"`
	Severity  string `json:"severity"`
	Message   string `json:"message"`
	ArticleID string `json:"article_id,omitempty"`
	Suggestion string `json:"suggestion,omitempty"`
}

// --- AST Parsing ---

// CodeModule holds extracted structure from a source file.
type CodeModule struct {
	Language  string       `json:"language"`  // go, python, typescript
	FilePath  string       `json:"file_path"`
	Package   string       `json:"package,omitempty"`
	Imports   []string     `json:"imports,omitempty"`
	Types     []CodeType   `json:"types,omitempty"`     // structs, classes, interfaces
	Functions []CodeFunc   `json:"functions,omitempty"`
	Constants []string     `json:"constants,omitempty"`
	Docstring string       `json:"docstring,omitempty"` // module/package doc
}

type CodeType struct {
	Name       string     `json:"name"`
	Kind       string     `json:"kind"` // struct, class, interface, type
	Bases      []string   `json:"bases,omitempty"`
	Methods    []CodeFunc `json:"methods,omitempty"`
	Fields     []string   `json:"fields,omitempty"`
	Docstring  string     `json:"docstring,omitempty"`
	IsExported bool       `json:"is_exported,omitempty"`
}

type CodeFunc struct {
	Name       string   `json:"name"`
	Args       []string `json:"args,omitempty"`
	Returns    string   `json:"returns,omitempty"`
	IsAsync    bool     `json:"is_async,omitempty"`
	IsExported bool     `json:"is_exported,omitempty"`
	Docstring  string   `json:"docstring,omitempty"`
}

// detectLanguage returns the language from file extension.
func detectLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go"
	case ".py":
		return "python"
	case ".ts", ".tsx":
		return "typescript"
	case ".js", ".jsx":
		return "javascript"
	default:
		return ""
	}
}

// parseCode dispatches to the right parser based on language.
func parseCode(path, source string) *CodeModule {
	lang := detectLanguage(path)
	switch lang {
	case "go":
		return parseGo(path, source)
	case "python":
		return parsePython(path, source)
	case "typescript", "javascript":
		return parseTypeScript(path, source, lang)
	default:
		return nil
	}
}

// --- Go Parser (stdlib go/ast) ---

func parseGo(path, source string) *CodeModule {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, source, parser.ParseComments)
	if err != nil {
		return nil
	}

	mod := &CodeModule{
		Language: "go",
		FilePath: path,
		Package:  f.Name.Name,
	}

	// Package doc
	if f.Doc != nil {
		mod.Docstring = f.Doc.Text()
	}

	// Imports
	for _, imp := range f.Imports {
		impPath := strings.Trim(imp.Path.Value, `"`)
		mod.Imports = append(mod.Imports, impPath)
	}

	// Walk declarations
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					ct := CodeType{
						Name:       s.Name.Name,
						IsExported: ast.IsExported(s.Name.Name),
					}
					if d.Doc != nil {
						ct.Docstring = d.Doc.Text()
					}
					switch st := s.Type.(type) {
					case *ast.StructType:
						ct.Kind = "struct"
						if st.Fields != nil {
							for _, field := range st.Fields.List {
								for _, name := range field.Names {
									ct.Fields = append(ct.Fields, name.Name)
								}
							}
						}
					case *ast.InterfaceType:
						ct.Kind = "interface"
						if st.Methods != nil {
							for _, m := range st.Methods.List {
								for _, name := range m.Names {
									ct.Methods = append(ct.Methods, CodeFunc{
										Name:       name.Name,
										IsExported: ast.IsExported(name.Name),
									})
								}
							}
						}
					default:
						ct.Kind = "type"
					}
					mod.Types = append(mod.Types, ct)

				case *ast.ValueSpec:
					for _, name := range s.Names {
						if ast.IsExported(name.Name) {
							mod.Constants = append(mod.Constants, name.Name)
						}
					}
				}
			}

		case *ast.FuncDecl:
			fn := CodeFunc{
				Name:       d.Name.Name,
				IsExported: ast.IsExported(d.Name.Name),
			}
			if d.Doc != nil {
				fn.Docstring = d.Doc.Text()
			}
			// Args
			if d.Type.Params != nil {
				for _, p := range d.Type.Params.List {
					for _, name := range p.Names {
						fn.Args = append(fn.Args, name.Name)
					}
				}
			}
			// Returns
			if d.Type.Results != nil {
				var rets []string
				for _, r := range d.Type.Results.List {
					if len(r.Names) > 0 {
						for _, name := range r.Names {
							rets = append(rets, name.Name)
						}
					} else {
						rets = append(rets, "...")
					}
				}
				fn.Returns = strings.Join(rets, ", ")
			}
			// Method receiver → attach to type
			if d.Recv != nil && len(d.Recv.List) > 0 {
				recvType := exprName(d.Recv.List[0].Type)
				attached := false
				for i := range mod.Types {
					if mod.Types[i].Name == recvType {
						mod.Types[i].Methods = append(mod.Types[i].Methods, fn)
						attached = true
						break
					}
				}
				if !attached {
					// Type not yet seen — create placeholder
					mod.Types = append(mod.Types, CodeType{
						Name:    recvType,
						Kind:    "struct",
						Methods: []CodeFunc{fn},
					})
				}
			} else {
				mod.Functions = append(mod.Functions, fn)
			}
		}
	}

	return mod
}

// exprName extracts the type name from a receiver expression (handles *T and T).
func exprName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return exprName(e.X)
	default:
		return ""
	}
}

// --- Python Parser (regex-based) ---

var (
	pyClassRe    = regexp.MustCompile(`(?m)^class\s+(\w+)\s*(?:\(([^)]*)\))?\s*:`)
	pyFuncRe     = regexp.MustCompile(`(?m)^(\s*)(async\s+)?def\s+(\w+)\s*\(([^)]*)\)(?:\s*->\s*([^\s:]+))?\s*:`)
	pyImportRe   = regexp.MustCompile(`(?m)^(?:from\s+(\S+)\s+)?import\s+(.+)$`)
	pyDocstringRe = regexp.MustCompile(`(?m)^\s*"""((?s:.*?))"""`)
	pyConstRe    = regexp.MustCompile(`(?m)^([A-Z][A-Z0-9_]+)\s*=`)
)

func parsePython(path, source string) *CodeModule {
	mod := &CodeModule{
		Language: "python",
		FilePath: path,
	}

	lines := strings.Split(source, "\n")

	// Module docstring (first triple-quoted string)
	if match := pyDocstringRe.FindStringSubmatch(source); match != nil {
		// Only count as module doc if it starts near the top (within first 5 non-empty lines)
		idx := strings.Index(source, match[0])
		prefix := source[:idx]
		nonEmpty := 0
		for _, l := range strings.Split(prefix, "\n") {
			if strings.TrimSpace(l) != "" && !strings.HasPrefix(strings.TrimSpace(l), "#") {
				nonEmpty++
			}
		}
		if nonEmpty == 0 {
			mod.Docstring = strings.TrimSpace(match[1])
		}
	}

	// Imports
	for _, match := range pyImportRe.FindAllStringSubmatch(source, -1) {
		if match[1] != "" {
			mod.Imports = append(mod.Imports, match[1])
		} else {
			for _, imp := range strings.Split(match[2], ",") {
				imp = strings.TrimSpace(imp)
				if imp != "" {
					mod.Imports = append(mod.Imports, imp)
				}
			}
		}
	}

	// Constants
	for _, match := range pyConstRe.FindAllStringSubmatch(source, -1) {
		mod.Constants = append(mod.Constants, match[1])
	}

	// Classes and their methods
	classLocs := pyClassRe.FindAllStringSubmatchIndex(source, -1)
	for i, loc := range classLocs {
		match := pyClassRe.FindStringSubmatch(source[loc[0]:loc[1]])
		ct := CodeType{
			Name: match[1],
			Kind: "class",
		}
		if match[2] != "" {
			for _, base := range strings.Split(match[2], ",") {
				base = strings.TrimSpace(base)
				if base != "" {
					ct.Bases = append(ct.Bases, base)
				}
			}
		}

		// Find class body (until next class or EOF)
		start := loc[1]
		end := len(source)
		if i+1 < len(classLocs) {
			end = classLocs[i+1][0]
		}
		classBody := source[start:end]

		// Class docstring
		if docMatch := pyDocstringRe.FindStringSubmatch(classBody); docMatch != nil {
			ct.Docstring = strings.TrimSpace(docMatch[1])
		}

		// Methods within class
		for _, fMatch := range pyFuncRe.FindAllStringSubmatch(classBody, -1) {
			indent := fMatch[1]
			if len(indent) < 4 { // not indented enough to be a method
				continue
			}
			fn := CodeFunc{
				Name:    fMatch[3],
				IsAsync: fMatch[2] != "",
			}
			// Args (skip self/cls)
			args := strings.Split(fMatch[4], ",")
			for _, arg := range args {
				arg = strings.TrimSpace(arg)
				arg = strings.SplitN(arg, ":", 2)[0]
				arg = strings.SplitN(arg, "=", 2)[0]
				arg = strings.TrimSpace(arg)
				if arg != "" && arg != "self" && arg != "cls" {
					fn.Args = append(fn.Args, arg)
				}
			}
			if fMatch[5] != "" {
				fn.Returns = fMatch[5]
			}
			ct.Methods = append(ct.Methods, fn)
		}

		mod.Types = append(mod.Types, ct)
	}

	// Top-level functions (not indented)
	for _, lineNum := range findLines(lines, pyFuncRe) {
		line := lines[lineNum]
		fMatch := pyFuncRe.FindStringSubmatch(line)
		if fMatch == nil || len(fMatch[1]) > 0 { // skip indented (methods)
			continue
		}
		fn := CodeFunc{
			Name:    fMatch[3],
			IsAsync: fMatch[2] != "",
		}
		args := strings.Split(fMatch[4], ",")
		for _, arg := range args {
			arg = strings.TrimSpace(arg)
			arg = strings.SplitN(arg, ":", 2)[0]
			arg = strings.SplitN(arg, "=", 2)[0]
			arg = strings.TrimSpace(arg)
			if arg != "" {
				fn.Args = append(fn.Args, arg)
			}
		}
		if fMatch[5] != "" {
			fn.Returns = fMatch[5]
		}
		// Check for docstring on next line
		if lineNum+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[lineNum+1])
			if strings.HasPrefix(nextLine, `"""`) {
				doc := strings.Trim(nextLine, `" `)
				fn.Docstring = doc
			}
		}
		fn.IsExported = !strings.HasPrefix(fn.Name, "_")
		mod.Functions = append(mod.Functions, fn)
	}

	return mod
}

func findLines(lines []string, re *regexp.Regexp) []int {
	var result []int
	for i, line := range lines {
		if re.MatchString(line) {
			result = append(result, i)
		}
	}
	return result
}

// --- TypeScript/JavaScript Parser (regex-based) ---

var (
	tsClassRe     = regexp.MustCompile(`(?m)^(?:export\s+)?(?:abstract\s+)?class\s+(\w+)(?:\s+extends\s+(\w+))?(?:\s+implements\s+([^{]+))?\s*\{`)
	tsInterfaceRe = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+(\w+)(?:\s+extends\s+([^{]+))?\s*\{`)
	tsFuncRe      = regexp.MustCompile(`(?m)^(?:export\s+)?(?:async\s+)?function\s+(\w+)\s*(?:<[^>]+>)?\s*\(([^)]*)\)(?:\s*:\s*([^\s{]+))?\s*\{`)
	tsArrowRe     = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const|let)\s+(\w+)\s*=\s*(?:async\s+)?\([^)]*\)(?:\s*:\s*[^\s=]+)?\s*=>`)
	tsImportRe    = regexp.MustCompile(`(?m)^import\s+(?:\{([^}]+)\}|(\w+))\s+from\s+['"]([^'"]+)['"]`)
	tsTypeRe      = regexp.MustCompile(`(?m)^(?:export\s+)?type\s+(\w+)(?:<[^>]+>)?\s*=`)
	tsEnumRe      = regexp.MustCompile(`(?m)^(?:export\s+)?(?:const\s+)?enum\s+(\w+)\s*\{`)
)

func parseTypeScript(path, source, lang string) *CodeModule {
	mod := &CodeModule{
		Language: lang,
		FilePath: path,
	}

	// Imports
	for _, match := range tsImportRe.FindAllStringSubmatch(source, -1) {
		mod.Imports = append(mod.Imports, match[3])
	}

	// Classes
	for _, match := range tsClassRe.FindAllStringSubmatch(source, -1) {
		ct := CodeType{
			Name:       match[1],
			Kind:       "class",
			IsExported: strings.Contains(match[0], "export"),
		}
		if match[2] != "" {
			ct.Bases = append(ct.Bases, strings.TrimSpace(match[2]))
		}
		if match[3] != "" {
			for _, impl := range strings.Split(match[3], ",") {
				ct.Bases = append(ct.Bases, strings.TrimSpace(impl))
			}
		}
		mod.Types = append(mod.Types, ct)
	}

	// Interfaces
	for _, match := range tsInterfaceRe.FindAllStringSubmatch(source, -1) {
		ct := CodeType{
			Name:       match[1],
			Kind:       "interface",
			IsExported: strings.Contains(match[0], "export"),
		}
		if match[2] != "" {
			for _, ext := range strings.Split(match[2], ",") {
				ct.Bases = append(ct.Bases, strings.TrimSpace(ext))
			}
		}
		mod.Types = append(mod.Types, ct)
	}

	// Type aliases
	for _, match := range tsTypeRe.FindAllStringSubmatch(source, -1) {
		mod.Types = append(mod.Types, CodeType{
			Name:       match[1],
			Kind:       "type",
			IsExported: strings.Contains(match[0], "export"),
		})
	}

	// Enums
	for _, match := range tsEnumRe.FindAllStringSubmatch(source, -1) {
		mod.Types = append(mod.Types, CodeType{
			Name:       match[1],
			Kind:       "enum",
			IsExported: strings.Contains(match[0], "export"),
		})
	}

	// Functions
	for _, match := range tsFuncRe.FindAllStringSubmatch(source, -1) {
		fn := CodeFunc{
			Name:       match[1],
			IsAsync:    strings.Contains(match[0], "async"),
			IsExported: strings.Contains(match[0], "export"),
		}
		if match[2] != "" {
			for _, arg := range strings.Split(match[2], ",") {
				arg = strings.TrimSpace(arg)
				arg = strings.SplitN(arg, ":", 2)[0]
				arg = strings.SplitN(arg, "=", 2)[0]
				arg = strings.TrimSpace(arg)
				if arg != "" {
					fn.Args = append(fn.Args, arg)
				}
			}
		}
		if match[3] != "" {
			fn.Returns = match[3]
		}
		mod.Functions = append(mod.Functions, fn)
	}

	// Arrow functions (exported const)
	for _, match := range tsArrowRe.FindAllStringSubmatch(source, -1) {
		fn := CodeFunc{
			Name:       match[1],
			IsAsync:    strings.Contains(match[0], "async"),
			IsExported: strings.Contains(match[0], "export"),
		}
		mod.Functions = append(mod.Functions, fn)
	}

	return mod
}

// formatCodeContext produces a structured summary for the LLM prompt.
func formatCodeContext(mod *CodeModule) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Language: %s\n", mod.Language)
	if mod.Package != "" {
		fmt.Fprintf(&sb, "Package: %s\n", mod.Package)
	}
	if mod.Docstring != "" {
		fmt.Fprintf(&sb, "Module doc: %s\n", mod.Docstring)
	}

	if len(mod.Imports) > 0 {
		fmt.Fprintf(&sb, "\nImports: %s\n", strings.Join(mod.Imports, ", "))
	}

	if len(mod.Types) > 0 {
		sb.WriteString("\nTypes:\n")
		for _, t := range mod.Types {
			exported := ""
			if t.IsExported {
				exported = " (exported)"
			}
			bases := ""
			if len(t.Bases) > 0 {
				bases = " extends " + strings.Join(t.Bases, ", ")
			}
			fmt.Fprintf(&sb, "  %s %s%s%s\n", t.Kind, t.Name, bases, exported)
			if t.Docstring != "" {
				fmt.Fprintf(&sb, "    doc: %s\n", truncate(t.Docstring, 100))
			}
			for _, f := range t.Fields {
				fmt.Fprintf(&sb, "    field: %s\n", f)
			}
			for _, m := range t.Methods {
				async := ""
				if m.IsAsync {
					async = "async "
				}
				fmt.Fprintf(&sb, "    %smethod: %s(%s)", async, m.Name, strings.Join(m.Args, ", "))
				if m.Returns != "" {
					fmt.Fprintf(&sb, " -> %s", m.Returns)
				}
				sb.WriteString("\n")
			}
		}
	}

	if len(mod.Functions) > 0 {
		sb.WriteString("\nFunctions:\n")
		for _, fn := range mod.Functions {
			async := ""
			if fn.IsAsync {
				async = "async "
			}
			exported := ""
			if fn.IsExported {
				exported = " (exported)"
			}
			fmt.Fprintf(&sb, "  %s%s(%s)", async, fn.Name, strings.Join(fn.Args, ", "))
			if fn.Returns != "" {
				fmt.Fprintf(&sb, " -> %s", fn.Returns)
			}
			fmt.Fprintf(&sb, "%s\n", exported)
			if fn.Docstring != "" {
				fmt.Fprintf(&sb, "    doc: %s\n", truncate(fn.Docstring, 100))
			}
		}
	}

	if len(mod.Constants) > 0 {
		fmt.Fprintf(&sb, "\nConstants: %s\n", strings.Join(mod.Constants, ", "))
	}

	return sb.String()
}

// --- Storage ---

func basePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, defaultBaseDir)
}

func scopeDir(scope string) string {
	safe := sanitize(scope)
	return filepath.Join(basePath(), safe)
}

func sanitize(s string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9_-]`)
	return re.ReplaceAllString(s, "_")
}

func ensureDirs(scope string) {
	root := scopeDir(scope)
	os.MkdirAll(filepath.Join(root, "raw"), 0o755)
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.MkdirAll(filepath.Join(root, "cache"), 0o755)
}

// --- Raw Doc Storage ---

func saveRawDoc(scope string, doc *RawDoc) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "raw", doc.ID+".json")
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadRawDoc(scope, id string) (*RawDoc, error) {
	path := filepath.Join(scopeDir(scope), "raw", id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc RawDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

// --- Article Storage ---

func saveArticle(scope string, a *WikiArticle) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "wiki", a.ID+".md")

	fm := Frontmatter{
		Title:        a.Title,
		Summary:      a.Summary,
		Concepts:     a.Concepts,
		Categories:   a.Categories,
		SourceDocs:   a.SourceDocs,
		Backlinks:    a.Backlinks,
		WordCount:    a.WordCount,
		CompiledAt:   a.CompiledAt,
		CompiledWith: a.CompiledWith,
		Version:      a.Version,
	}
	fmData, err := json.MarshalIndent(fm, "", "  ")
	if err != nil {
		return err
	}

	content := fmt.Sprintf("---\n%s\n---\n\n%s", string(fmData), a.Content)
	return os.WriteFile(path, []byte(content), 0o644)
}

func loadArticle(scope, id string) (*WikiArticle, error) {
	path := filepath.Join(scopeDir(scope), "wiki", id+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseArticle(id, string(data))
}

func parseArticle(id, text string) (*WikiArticle, error) {
	if !strings.HasPrefix(text, "---") {
		return &WikiArticle{
			ID:        id,
			Title:     id,
			Content:   text,
			WordCount: wordCount(text),
			Version:   1,
		}, nil
	}

	parts := strings.SplitN(text, "---", 3)
	if len(parts) < 3 {
		return &WikiArticle{
			ID:        id,
			Title:     id,
			Content:   text,
			WordCount: wordCount(text),
			Version:   1,
		}, nil
	}

	var fm Frontmatter
	if err := json.Unmarshal([]byte(parts[1]), &fm); err != nil {
		return nil, fmt.Errorf("bad frontmatter in %s: %w", id, err)
	}

	content := strings.TrimSpace(parts[2])
	return &WikiArticle{
		ID:           id,
		Title:        fm.Title,
		Summary:      fm.Summary,
		Content:      content,
		Concepts:     nilToEmpty(fm.Concepts),
		Categories:   nilToEmpty(fm.Categories),
		SourceDocs:   nilToEmpty(fm.SourceDocs),
		Backlinks:    nilToEmpty(fm.Backlinks),
		WordCount:    fm.WordCount,
		CompiledAt:   fm.CompiledAt,
		CompiledWith: fm.CompiledWith,
		Version:      fm.Version,
	}, nil
}

func listArticles(scope string) ([]*WikiArticle, error) {
	dir := filepath.Join(scopeDir(scope), "wiki")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var articles []*WikiArticle
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		a, err := parseArticle(id, string(data))
		if err != nil {
			continue
		}
		articles = append(articles, a)
	}
	sort.Slice(articles, func(i, j int) bool { return articles[i].ID < articles[j].ID })
	return articles, nil
}

// --- Index Storage ---

func loadIndex(scope string) *KnowledgeIndex {
	path := filepath.Join(scopeDir(scope), "index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &KnowledgeIndex{
			Scope:    scope,
			Articles: map[string]any{},
			Concepts: map[string]*Concept{},
		}
	}
	var idx KnowledgeIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return &KnowledgeIndex{
			Scope:    scope,
			Articles: map[string]any{},
			Concepts: map[string]*Concept{},
		}
	}
	if idx.Articles == nil {
		idx.Articles = map[string]any{}
	}
	if idx.Concepts == nil {
		idx.Concepts = map[string]*Concept{}
	}
	return &idx
}

func saveIndex(scope string, idx *KnowledgeIndex) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "index.json")
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func rebuildIndex(scope string, articles []*WikiArticle) *KnowledgeIndex {
	idx := &KnowledgeIndex{
		Scope:    scope,
		Articles: map[string]any{},
		Concepts: map[string]*Concept{},
	}
	catSet := map[string]bool{}

	for _, a := range articles {
		idx.Articles[a.ID] = map[string]any{
			"title":   a.Title,
			"summary": a.Summary,
		}

		for _, c := range a.Concepts {
			key := strings.ToLower(strings.TrimSpace(c))
			if key == "" {
				continue
			}
			concept, ok := idx.Concepts[key]
			if !ok {
				concept = &Concept{Name: c}
				idx.Concepts[key] = concept
			}
			if !contains(concept.Articles, a.ID) {
				concept.Articles = append(concept.Articles, a.ID)
			}
		}

		for _, cat := range a.Categories {
			catSet[cat] = true
		}
	}

	for cat := range catSet {
		idx.Categories = append(idx.Categories, cat)
	}
	sort.Strings(idx.Categories)

	return idx
}

// --- Cache ---

func loadCache(scope string) *Cache {
	path := filepath.Join(scopeDir(scope), "cache", "hashes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return &Cache{Version: 1, Files: map[string]CacheEntry{}}
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return &Cache{Version: 1, Files: map[string]CacheEntry{}}
	}
	if c.Files == nil {
		c.Files = map[string]CacheEntry{}
	}
	return &c
}

func saveCache(scope string, c *Cache) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "cache", "hashes.json")
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func contentHash(text string) string {
	h := sha256.Sum256([]byte(text))
	return fmt.Sprintf("%x", h)
}

// --- Wiki Export ---

func exportWiki(scope, outputDir string) {
	articles, _ := listArticles(scope)
	idx := loadIndex(scope)

	os.MkdirAll(outputDir, 0o755)

	// Write each article as a standalone .md
	for _, a := range articles {
		var sb strings.Builder
		fmt.Fprintf(&sb, "# %s\n\n", a.Title)
		if a.Summary != "" {
			fmt.Fprintf(&sb, "> %s\n\n", a.Summary)
		}
		if len(a.Categories) > 0 {
			fmt.Fprintf(&sb, "**Categories:** %s  \n", strings.Join(a.Categories, ", "))
		}
		if len(a.Concepts) > 0 {
			top := a.Concepts
			if len(top) > 10 {
				top = top[:10]
			}
			fmt.Fprintf(&sb, "**Concepts:** %s  \n", strings.Join(top, ", "))
		}
		fmt.Fprintf(&sb, "**Words:** %d | **Version:** %d\n\n---\n\n", a.WordCount, a.Version)
		sb.WriteString(a.Content)
		if len(a.Backlinks) > 0 {
			sb.WriteString("\n\n---\n\n## Related\n\n")
			for _, link := range a.Backlinks {
				fmt.Fprintf(&sb, "- [%s](%s.md)\n", link, link)
			}
		}
		os.WriteFile(filepath.Join(outputDir, a.ID+".md"), []byte(sb.String()), 0o644)
	}

	// Write index.md
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Knowledge Base: %s\n\n", scope)
	fmt.Fprintf(&sb, "**%d articles** | **%d concepts** | **%d categories**\n\n", len(articles), len(idx.Concepts), len(idx.Categories))

	if len(idx.Categories) > 0 {
		sb.WriteString("## Categories\n\n")
		for _, cat := range idx.Categories {
			fmt.Fprintf(&sb, "### %s\n\n", cat)
			for _, a := range articles {
				if contains(a.Categories, cat) {
					fmt.Fprintf(&sb, "- [%s](%s.md) — %s\n", a.Title, a.ID, truncate(a.Summary, 80))
				}
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("## Articles\n\n")
		for _, a := range articles {
			fmt.Fprintf(&sb, "- [%s](%s.md) — %s\n", a.Title, a.ID, truncate(a.Summary, 80))
		}
	}
	os.WriteFile(filepath.Join(outputDir, "index.md"), []byte(sb.String()), 0o644)

	fmt.Printf("Exported %d articles + index.md to %s/\n", len(articles), outputDir)
}

// --- Search Index (pre-tokenized) ---

// SearchIndex stores pre-tokenized article data for fast BM25 search.
type SearchIndex struct {
	Articles []SearchEntry `json:"articles"`
	AvgDL    float64       `json:"avg_dl"`
}

type SearchEntry struct {
	ID            string   `json:"id"`
	AllTokens     []string `json:"all"`
	TitleTokens   []string `json:"title"`
	ConceptTokens []string `json:"concepts"`
}

func buildSearchIndex(articles []*WikiArticle) *SearchIndex {
	si := &SearchIndex{Articles: make([]SearchEntry, len(articles))}
	totalLen := 0
	for i, a := range articles {
		all := tokenize(a.Title + " " + a.Summary + " " + a.Content +
			" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
		si.Articles[i] = SearchEntry{
			ID:            a.ID,
			AllTokens:     all,
			TitleTokens:   tokenize(a.Title),
			ConceptTokens: tokenize(strings.Join(a.Concepts, " ")),
		}
		totalLen += len(all)
	}
	if len(articles) > 0 {
		si.AvgDL = float64(totalLen) / float64(len(articles))
	}
	return si
}

func saveSearchIndex(scope string, si *SearchIndex) error {
	ensureDirs(scope)
	path := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	data, err := json.Marshal(si)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadSearchIndex(scope string) *SearchIndex {
	path := filepath.Join(scopeDir(scope), "cache", "search_index.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var si SearchIndex
	if err := json.Unmarshal(data, &si); err != nil {
		return nil
	}
	return &si
}

// --- BM25 Search ---

func tokenize(text string) []string {
	lower := strings.ToLower(text)
	splitter := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	}
	tokens := strings.FieldsFunc(lower, splitter)
	return tokens
}

func bm25Search(articles []*WikiArticle, query string, limit int) []*WikiArticle {
	return bm25SearchWithIndex(articles, query, limit, nil)
}

func bm25SearchWithIndex(articles []*WikiArticle, query string, limit int, si *SearchIndex) []*WikiArticle {
	if len(articles) == 0 || query == "" {
		return nil
	}

	queryTerms := tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	// Use pre-tokenized index if available, otherwise tokenize on the fly
	var docs [][]string
	var titleTokens, conceptTokens [][]string
	var avgDL float64

	if si != nil && len(si.Articles) == len(articles) {
		// Fast path: use cached tokens
		docs = make([][]string, len(si.Articles))
		titleTokens = make([][]string, len(si.Articles))
		conceptTokens = make([][]string, len(si.Articles))
		for i, entry := range si.Articles {
			docs[i] = entry.AllTokens
			titleTokens[i] = entry.TitleTokens
			conceptTokens[i] = entry.ConceptTokens
		}
		avgDL = si.AvgDL
	} else {
		// Slow path: tokenize everything
		docs = make([][]string, len(articles))
		titleTokens = make([][]string, len(articles))
		conceptTokens = make([][]string, len(articles))
		totalLen := 0
		for i, a := range articles {
			docs[i] = tokenize(a.Title + " " + a.Summary + " " + a.Content +
				" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
			titleTokens[i] = tokenize(a.Title)
			conceptTokens[i] = tokenize(strings.Join(a.Concepts, " "))
			totalLen += len(docs[i])
		}
		avgDL = float64(totalLen) / float64(len(docs))
	}

	// IDF per query term
	idfs := map[string]float64{}
	for _, term := range queryTerms {
		df := 0
		for _, doc := range docs {
			if containsStr(doc, term) {
				df++
			}
		}
		idfs[term] = math.Log((float64(len(docs))-float64(df)+0.5)/(float64(df)+0.5) + 1)
	}

	// Score each doc with title (3x) and concept (2x) boosting
	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(articles))
	for i, doc := range docs {
		s := 0.0
		dl := float64(len(doc))
		for _, term := range queryTerms {
			tf := float64(countStr(doc, term))
			num := tf * (bm25K1 + 1)
			den := tf + bm25K1*(1-bm25B+bm25B*dl/avgDL)
			base := idfs[term] * num / den
			s += base

			// Title boost: 3x for terms appearing in the title
			if containsStr(titleTokens[i], term) {
				s += base * 2.0
			}
			// Concept boost: 2x for terms matching concepts
			if containsStr(conceptTokens[i], term) {
				s += base * 1.0
			}
		}
		scores[i] = scored{i, s}
	}

	sort.Slice(scores, func(i, j int) bool { return scores[i].score > scores[j].score })

	var result []*WikiArticle
	for _, sc := range scores {
		if sc.score <= 0 {
			break
		}
		result = append(result, articles[sc.idx])
		if len(result) >= limit {
			break
		}
	}
	return result
}

// --- LLM Compilation ---

// TokenUsage tracks API token consumption.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

func compileLLM(rawText, source, model, apiKey string, codeMod *CodeModule) (*WikiArticle, *TokenUsage, error) {
	if apiKey == "" {
		return nil, nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	var contextBlock string
	if codeMod != nil {
		contextBlock = fmt.Sprintf("\nAST-extracted structure:\n```\n%s```\n\n", formatCodeContext(codeMod))
	}

	prompt := fmt.Sprintf(`Compile this source into a structured knowledge article.
Source: %s
%s
Important:
- Explain WHY code exists, not just what it does. For defensive patterns, edge cases, and workarounds, explain what failure they prevent.
- Flag any TODO, FIXME, HACK, or incomplete implementations as "Known Gaps" in the content.
- If you see idempotency guards, retry logic, or error handling, explain the failure scenario that motivated them.

Output ONLY valid JSON with these exact keys:
{"title":"descriptive title","summary":"2-3 sentence overview","content":"full markdown article with a Known Gaps section if applicable","concepts":["key","entities"],"categories":["broad","topics"]}

Source text:
%s`, source, contextBlock, rawText)

	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     "You are a knowledge compiler. Output only valid JSON. No markdown fences.",
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse Anthropic response with usage
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	usage := &TokenUsage{
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
	}

	if len(apiResp.Content) == 0 {
		return nil, usage, fmt.Errorf("empty API response")
	}

	text := apiResp.Content[0].Text
	// Strip markdown code fences if present
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result struct {
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Content    string   `json:"content"`
		Concepts   []string `json:"concepts"`
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, usage, fmt.Errorf("failed to parse LLM output as JSON: %w\nraw: %s", err, text[:min(len(text), 200)])
	}

	slug := slugify(result.Title)
	now := time.Now().UTC().Format(time.RFC3339)

	return &WikiArticle{
		ID:           slug,
		Title:        result.Title,
		Summary:      result.Summary,
		Content:      result.Content,
		Concepts:     nilToEmpty(result.Concepts),
		Categories:   nilToEmpty(result.Categories),
		SourceDocs:   nil,
		Backlinks:    nil,
		WordCount:    wordCount(result.Content),
		CompiledAt:   now,
		CompiledWith: model,
		Version:      1,
	}, usage, nil
}

// --- Structural Lint (no LLM) ---

func lintStructural(scope string) []LintIssue {
	articles, _ := listArticles(scope)
	idx := loadIndex(scope)
	var issues []LintIssue

	if len(articles) == 0 {
		issues = append(issues, LintIssue{
			Type: "gap", Severity: "warning",
			Message: "Knowledge base is empty — no articles found.",
		})
		return issues
	}

	articleIDs := map[string]bool{}
	for _, a := range articles {
		articleIDs[a.ID] = true
	}

	for _, a := range articles {
		// Check for empty content
		if strings.TrimSpace(a.Content) == "" {
			issues = append(issues, LintIssue{
				Type: "gap", Severity: "error",
				Message:   fmt.Sprintf("Article '%s' has empty content", a.Title),
				ArticleID: a.ID,
			})
		}

		// Check for missing concepts
		if len(a.Concepts) == 0 {
			issues = append(issues, LintIssue{
				Type: "gap", Severity: "warning",
				Message:    fmt.Sprintf("Article '%s' has no concepts", a.Title),
				ArticleID:  a.ID,
				Suggestion: "Recompile to extract concepts",
			})
		}

		// Check for broken backlinks
		for _, link := range a.Backlinks {
			if !articleIDs[link] {
				issues = append(issues, LintIssue{
					Type: "connection", Severity: "warning",
					Message:    fmt.Sprintf("Article '%s' has broken backlink to '%s'", a.Title, link),
					ArticleID:  a.ID,
					Suggestion: "Remove broken backlink or create missing article",
				})
			}
		}

		// Check for missing summary
		if strings.TrimSpace(a.Summary) == "" {
			issues = append(issues, LintIssue{
				Type: "gap", Severity: "info",
				Message:    fmt.Sprintf("Article '%s' has no summary", a.Title),
				ArticleID:  a.ID,
				Suggestion: "Recompile to generate summary",
			})
		}
	}

	// Check for orphan concepts (in index but no articles reference them)
	for key, c := range idx.Concepts {
		alive := false
		for _, aid := range c.Articles {
			if articleIDs[aid] {
				alive = true
				break
			}
		}
		if !alive {
			issues = append(issues, LintIssue{
				Type: "stale", Severity: "info",
				Message:    fmt.Sprintf("Concept '%s' (%s) has no live articles", c.Name, key),
				Suggestion: "Rebuild index to clean up",
			})
		}
	}

	// Check for island articles (no backlinks to or from)
	for _, a := range articles {
		if len(a.Backlinks) == 0 {
			linkedTo := false
			for _, other := range articles {
				if other.ID == a.ID {
					continue
				}
				if contains(other.Backlinks, a.ID) {
					linkedTo = true
					break
				}
			}
			if !linkedTo && len(articles) > 1 {
				issues = append(issues, LintIssue{
					Type: "connection", Severity: "info",
					Message:    fmt.Sprintf("Article '%s' is isolated (no backlinks)", a.Title),
					ArticleID:  a.ID,
					Suggestion: "Consider linking to related articles",
				})
			}
		}
	}

	return issues
}

// --- LLM Lint ---

func lintLLM(scope, model, apiKey string) ([]LintIssue, error) {
	articles, _ := listArticles(scope)
	if len(articles) == 0 {
		return []LintIssue{{
			Type: "gap", Severity: "warning",
			Message: "Knowledge base is empty.",
		}}, nil
	}

	// Build summary for LLM
	var sb strings.Builder
	for _, a := range articles {
		fmt.Fprintf(&sb, "## %s (id: %s)\nSummary: %s\nConcepts: %s\nCategories: %s\nBacklinks: %s\n\n",
			a.Title, a.ID, a.Summary,
			strings.Join(a.Concepts, ", "),
			strings.Join(a.Categories, ", "),
			strings.Join(a.Backlinks, ", "))
	}

	prompt := fmt.Sprintf(`Review this knowledge base and find issues.

Look for:
- INCONSISTENCY: articles that contradict each other
- GAP: important topics mentioned but not covered by any article
- CONNECTION: related articles that should reference each other but don't
- STALE: articles that seem outdated or need recompilation

Output ONLY a JSON array:
[{"type":"gap","severity":"warning","message":"...","article_id":"...","suggestion":"..."}]

If no issues, output: []

Knowledge base:
%s`, sb.String())

	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     "You are a knowledge base auditor. Output only valid JSON arrays.",
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})

	req, _ := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	var apiResp struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	json.Unmarshal(respBody, &apiResp)
	if len(apiResp.Content) == 0 {
		return nil, fmt.Errorf("empty response")
	}

	text := apiResp.Content[0].Text
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var issues []LintIssue
	if err := json.Unmarshal([]byte(text), &issues); err != nil {
		return nil, fmt.Errorf("failed to parse lint output: %w", err)
	}
	return issues, nil
}

// --- CLI Commands ---

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
	outputDir := flagStr(args, "--output", "")
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

			// Parse AST if supported language
			codeMod := parseCode(j.filePath, j.text)

			// Compile with LLM
			article, usage, err := compileLLM(j.text, j.relPath, model, apiKey, codeMod)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: compilation failed for %s: %v\n", j.relPath, err)
				article = &WikiArticle{
					ID:           slugify(filepath.Base(j.filePath)),
					Title:        filepath.Base(j.filePath),
					Summary:      truncate(j.text, 200),
					Content:      j.text,
					WordCount:    wordCount(j.text),
					CompiledAt:   time.Now().UTC().Format(time.RFC3339),
					CompiledWith: "none (fallback)",
					Version:      1,
				}
			}
			article.SourceDocs = []string{j.rawID}

			// Auto-tag test files
			if isTestFile(j.filePath) && !contains(article.Categories, "test") {
				article.Categories = append(article.Categories, "test")
			}

			mu.Lock()
			results = append(results, compileResult{job: j, article: article, usage: usage})
			mu.Unlock()
		}(job)
	}
	wg.Wait()

	// Phase 3: save articles + update cache (sequential for consistency)
	changed := len(results)
	var totalInput, totalOutput int
	for _, r := range results {
		if r.usage != nil {
			totalInput += r.usage.InputTokens
			totalOutput += r.usage.OutputTokens
		}
		if existing, err := loadArticle(scope, r.article.ID); err == nil && existing != nil {
			r.article.Version = existing.Version + 1
		}
		saveArticle(scope, r.article)
		cache.Files[r.job.relPath] = CacheEntry{
			Hash:       r.job.hash,
			ArticleID:  r.article.ID,
			CompiledAt: r.article.CompiledAt,
		}
	}

	saveCache(scope, cache)

	// Rebuild index
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	if jsonOut {
		out := map[string]any{
			"changed":       changed,
			"cached":        skipped,
			"total":         len(files),
			"articles":      len(allArticles),
			"input_tokens":  totalInput,
			"output_tokens": totalOutput,
			"total_tokens":  totalInput + totalOutput,
		}
		printJSON(out)
	} else {
		fmt.Printf("\nBuilt: %d compiled, %d cached (skipped), %d total files\n", changed, skipped, len(files))
		fmt.Printf("KB: %d articles, %d concepts\n", len(allArticles), len(idx.Concepts))
		if totalInput > 0 {
			fmt.Printf("Tokens: %d input + %d output = %d total\n", totalInput, totalOutput, totalInput+totalOutput)
		}
	}

	// Export wiki to output directory if specified
	if outputDir != "" {
		exportWiki(scope, outputDir)
	}
}

// cmdPrepare scans files and outputs compilation prompts as JSON.
// Used in agent mode — the calling agent (Claude Code, Cursor, etc.)
// processes each prompt using its own LLM, then pipes results to `kb accept`.
// No API key needed.
func cmdPrepare(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb prepare <path> [--scope NAME] [--pattern GLOB] [--exclude GLOB]")
	}

	path := args[0]
	scope := flagStr(args, "--scope", filepath.Base(path))
	pattern := flagStr(args, "--pattern", "*.py")
	exclude := flagStr(args, "--exclude", "")

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

	type prepareItem struct {
		Source  string `json:"source"`
		Hash   string `json:"hash"`
		RawID  string `json:"raw_id"`
		Prompt string `json:"prompt"`
		IsTest bool   `json:"is_test"`
	}

	var items []prepareItem
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

		if entry, ok := cache.Files[relPath]; ok && entry.Hash == hash {
			skipped++
			continue
		}

		// Save raw doc
		rawID := hash[:16]
		raw := &RawDoc{
			ID:          rawID,
			SourceType:  "file",
			Source:      relPath,
			Filename:    filepath.Base(f),
			ContentType: "text",
			RawText:     string(text),
			WordCount:   wordCount(string(text)),
			IngestedAt:  time.Now().UTC().Format(time.RFC3339),
		}
		saveRawDoc(scope, raw)

		// Build the same prompt compileLLM would use
		codeMod := parseCode(f, string(text))
		var contextBlock string
		if codeMod != nil {
			contextBlock = fmt.Sprintf("\nAST-extracted structure:\n```\n%s```\n\n", formatCodeContext(codeMod))
		}

		prompt := fmt.Sprintf(`Compile this source into a structured knowledge article.
Source: %s
%s
Important:
- Explain WHY code exists, not just what it does. For defensive patterns, edge cases, and workarounds, explain what failure they prevent.
- Flag any TODO, FIXME, HACK, or incomplete implementations as "Known Gaps" in the content.
- If you see idempotency guards, retry logic, or error handling, explain the failure scenario that motivated them.

Output ONLY valid JSON with these exact keys:
{"title":"descriptive title","summary":"2-3 sentence overview","content":"full markdown article with a Known Gaps section if applicable","concepts":["key","entities"],"categories":["broad","topics"]}

Source text:
%s`, relPath, contextBlock, string(text))

		items = append(items, prepareItem{
			Source: relPath,
			Hash:   hash,
			RawID:  rawID,
			Prompt: prompt,
			IsTest: isTestFile(f),
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

// cmdAccept reads compiled article results from stdin and saves them.
// Companion to `kb prepare` — accepts the agent's compilation output.
// Input: JSON object with "scope" and "articles" array.
// Each article: {"source", "hash", "raw_id", "title", "summary", "content", "concepts", "categories"}
func cmdAccept(args []string) {
	scope := flagStr(args, "--scope", "")

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		fatal("Failed to read stdin: %v", err)
	}

	// Accept either a single object with "articles" array, or a bare array
	type acceptArticle struct {
		Source     string   `json:"source"`
		Hash       string   `json:"hash"`
		RawID      string   `json:"raw_id"`
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Content    string   `json:"content"`
		Concepts   []string `json:"concepts"`
		Categories []string `json:"categories"`
		IsTest     bool     `json:"is_test"`
	}

	var articles []acceptArticle

	// Try wrapped format first: {"scope": "...", "articles": [...]}
	var wrapped struct {
		Scope    string          `json:"scope"`
		Articles []acceptArticle `json:"articles"`
	}
	if err := json.Unmarshal(data, &wrapped); err == nil && len(wrapped.Articles) > 0 {
		articles = wrapped.Articles
		if scope == "" {
			scope = wrapped.Scope
		}
	} else {
		// Try bare array
		if err := json.Unmarshal(data, &articles); err != nil {
			// Try single object
			var single acceptArticle
			if err := json.Unmarshal(data, &single); err != nil {
				fatal("Failed to parse input. Expected JSON with articles array, array, or single article object.")
			}
			articles = []acceptArticle{single}
		}
	}

	if scope == "" {
		scope = "default"
	}

	ensureDirs(scope)
	cache := loadCache(scope)
	saved := 0

	for _, a := range articles {
		if a.Title == "" || a.Content == "" {
			fmt.Fprintf(os.Stderr, "Warning: skipping article for %s (missing title or content)\n", a.Source)
			continue
		}

		slug := slugify(a.Title)
		now := time.Now().UTC().Format(time.RFC3339)

		article := &WikiArticle{
			ID:           slug,
			Title:        a.Title,
			Summary:      a.Summary,
			Content:      a.Content,
			Concepts:     nilToEmpty(a.Concepts),
			Categories:   nilToEmpty(a.Categories),
			SourceDocs:   []string{a.RawID},
			WordCount:    wordCount(a.Content),
			CompiledAt:   now,
			CompiledWith: "agent",
			Version:      1,
		}

		// Auto-tag test files
		if a.IsTest && !contains(article.Categories, "test") {
			article.Categories = append(article.Categories, "test")
		}

		if existing, err := loadArticle(scope, article.ID); err == nil && existing != nil {
			article.Version = existing.Version + 1
		}
		saveArticle(scope, article)

		// Update cache
		if a.Hash != "" && a.Source != "" {
			cache.Files[a.Source] = CacheEntry{
				Hash:       a.Hash,
				ArticleID:  article.ID,
				CompiledAt: now,
			}
		}
		saved++
	}

	saveCache(scope, cache)

	// Rebuild index
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	output := map[string]any{
		"accepted": saved,
		"articles": len(allArticles),
		"concepts": len(idx.Concepts),
	}
	printJSON(output)
}

func cmdSearch(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb search <query> [--scope NAME] [--limit N] [--context] [--exclude-tags TAG]")
	}

	query := args[0]
	scope := flagStr(args, "--scope", "default")
	limit := flagInt(args, "--limit", 5)
	jsonOut := flagBool(args, "--json")
	contextMode := flagBool(args, "--context")
	excludeTags := flagStr(args, "--exclude-tags", "")

	// Resolve scopes: "*" = all, "a,b,c" = specific, else single
	scopes := resolveScopes(scope)

	// Collect articles from all scopes
	type scopedArticle struct {
		article *WikiArticle
		scope   string
	}
	var allArticles []*WikiArticle
	var scopeMap []string // parallel array: scope per article
	for _, s := range scopes {
		articles, err := listArticles(s)
		if err != nil {
			continue
		}
		for _, a := range articles {
			allArticles = append(allArticles, a)
			scopeMap = append(scopeMap, s)
		}
	}

	// Filter by excluded tags
	if excludeTags != "" {
		excluded := strings.Split(excludeTags, ",")
		var filtered []*WikiArticle
		var filteredScopes []string
		for i, a := range allArticles {
			skip := false
			for _, tag := range excluded {
				tag = strings.TrimSpace(tag)
				if contains(a.Categories, tag) {
					skip = true
					break
				}
			}
			if !skip {
				filtered = append(filtered, a)
				filteredScopes = append(filteredScopes, scopeMap[i])
			}
		}
		allArticles = filtered
		scopeMap = filteredScopes
	}

	// Search with pre-tokenized index (only works for single scope)
	var results []*WikiArticle
	if len(scopes) == 1 {
		si := loadSearchIndex(scopes[0])
		results = bm25SearchWithIndex(allArticles, query, limit, si)
	} else {
		results = bm25Search(allArticles, query, limit)
	}

	if contextMode {
		// Output formatted context for agent prompt injection
		var parts []string
		total := 0
		for _, a := range results {
			text := a.Content
			if len(text) > 2000 {
				text = fmt.Sprintf("## %s\n%s", a.Title, a.Summary)
			}
			block := fmt.Sprintf("## %s\n%s", a.Title, text)
			if total+len(block) > 8000 {
				break
			}
			parts = append(parts, block)
			total += len(block)
		}
		fmt.Print(strings.Join(parts, "\n\n---\n\n"))
		return
	}

	// Build a result-to-scope lookup for multi-scope display
	resultScope := func(a *WikiArticle) string {
		for i, art := range allArticles {
			if art == a && i < len(scopeMap) {
				return scopeMap[i]
			}
		}
		return ""
	}
	multiScope := len(scopes) > 1

	if jsonOut {
		out := make([]map[string]any, 0, len(results))
		for _, a := range results {
			entry := map[string]any{
				"id":       a.ID,
				"title":    a.Title,
				"summary":  a.Summary,
				"concepts": a.Concepts,
			}
			if multiScope {
				entry["scope"] = resultScope(a)
			}
			out = append(out, entry)
		}
		printJSON(out)
	} else {
		if len(results) == 0 {
			fmt.Println("No results found.")
			return
		}
		fmt.Printf("Found %d results:\n\n", len(results))
		for i, a := range results {
			scopeLabel := ""
			if multiScope {
				scopeLabel = fmt.Sprintf(" [%s]", resultScope(a))
			}
			fmt.Printf("  %d. %s%s\n", i+1, a.Title, scopeLabel)
			fmt.Printf("     %s\n", truncate(a.Summary, 120))
			if len(a.Concepts) > 0 {
				fmt.Printf("     Concepts: %s\n", strings.Join(a.Concepts[:min(len(a.Concepts), 5)], ", "))
			}
			fmt.Println()
		}
	}
}

func cmdIngest(args []string) {
	scope := flagStr(args, "--scope", "default")
	source := flagStr(args, "--source", "manual")
	model := flagStr(args, "--model", defaultModel)
	jsonOut := flagBool(args, "--json")
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	ensureDirs(scope)

	var text string

	// Check for non-flag argument (file path)
	// Skip flag values: if previous arg was a flag that takes a value, skip this one
	filePath := ""
	flagsWithValues := map[string]bool{"--scope": true, "--source": true, "--model": true, "--lang": true}
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

	if strings.TrimSpace(text) == "" {
		fatal("No content to ingest")
	}

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
	saveRawDoc(scope, raw)

	// Parse AST if it's a code file
	lang := flagStr(args, "--lang", "")
	var codeMod *CodeModule
	if filePath != "" {
		codeMod = parseCode(filePath, text)
	} else if lang != "" {
		// Use --lang flag for stdin input (e.g., --lang go)
		fakeFile := "stdin." + langToExt(lang)
		codeMod = parseCode(fakeFile, text)
	}

	// Compile
	article, _, err := compileLLM(text, source, model, apiKey, codeMod)
	if err != nil {
		// Fallback
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
	article.SourceDocs = []string{raw.ID}

	if existing, err := loadArticle(scope, article.ID); err == nil && existing != nil {
		article.Version = existing.Version + 1
	}

	saveArticle(scope, article)

	// Update index
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	if jsonOut {
		printJSON(map[string]any{
			"article": article.ID,
			"title":   article.Title,
			"words":   article.WordCount,
		})
	} else {
		fmt.Printf("Ingested: %s (%d words)\n", article.Title, article.WordCount)
		if len(article.Concepts) > 0 {
			fmt.Printf("  Concepts: %s\n", strings.Join(article.Concepts, ", "))
		}
	}
}

func cmdShow(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb show <article_id> [--scope NAME]")
	}
	id := args[0]
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	a, err := loadArticle(scope, id)
	if err != nil || a == nil {
		fatal("Article not found: %s", id)
	}

	if jsonOut {
		printJSON(map[string]any{
			"id":            a.ID,
			"title":         a.Title,
			"summary":       a.Summary,
			"content":       a.Content,
			"concepts":      a.Concepts,
			"categories":    a.Categories,
			"backlinks":     a.Backlinks,
			"word_count":    a.WordCount,
			"compiled_with": a.CompiledWith,
			"version":       a.Version,
		})
	} else {
		fmt.Printf("# %s\n", a.Title)
		fmt.Printf("ID: %s | Version: %d | Words: %d\n", a.ID, a.Version, a.WordCount)
		if len(a.Concepts) > 0 {
			fmt.Printf("Concepts: %s\n", strings.Join(a.Concepts, ", "))
		}
		if len(a.Categories) > 0 {
			fmt.Printf("Categories: %s\n", strings.Join(a.Categories, ", "))
		}
		fmt.Printf("Compiled with: %s\n", a.CompiledWith)
		fmt.Print("\n---\n\n")
		fmt.Println(a.Content)
	}
}

func cmdList(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	articles, _ := listArticles(scope)
	if len(articles) == 0 {
		if jsonOut {
			fmt.Println("[]")
		} else {
			fmt.Println("No articles in knowledge base.")
		}
		return
	}

	if jsonOut {
		var out []map[string]any
		for _, a := range articles {
			out = append(out, map[string]any{
				"id":            a.ID,
				"title":         a.Title,
				"summary":       truncate(a.Summary, 120),
				"word_count":    a.WordCount,
				"compiled_with": a.CompiledWith,
				"version":       a.Version,
			})
		}
		printJSON(out)
	} else {
		fmt.Printf("Articles (%d):\n\n", len(articles))
		for _, a := range articles {
			fmt.Printf("  [%s] %s\n", a.ID, a.Title)
			fmt.Printf("    %s\n", truncate(a.Summary, 100))
			fmt.Printf("    Words: %d | Version: %d | Compiled: %s\n\n", a.WordCount, a.Version, a.CompiledWith)
		}
	}
}

func cmdStats(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	articles, _ := listArticles(scope)
	idx := loadIndex(scope)
	rawCount := 0
	rawDir := filepath.Join(scopeDir(scope), "raw")
	if entries, err := os.ReadDir(rawDir); err == nil {
		rawCount = len(entries)
	}
	totalWords := 0
	for _, a := range articles {
		totalWords += a.WordCount
	}

	if jsonOut {
		printJSON(map[string]any{
			"scope":      scope,
			"articles":   len(articles),
			"raw_docs":   rawCount,
			"words":      totalWords,
			"concepts":   len(idx.Concepts),
			"categories": len(idx.Categories),
		})
	} else {
		fmt.Printf("Knowledge Base: %s\n", scope)
		fmt.Printf("  Articles:   %d\n", len(articles))
		fmt.Printf("  Raw docs:   %d\n", rawCount)
		fmt.Printf("  Words:      %d\n", totalWords)
		fmt.Printf("  Concepts:   %d\n", len(idx.Concepts))
		fmt.Printf("  Categories: %d\n", len(idx.Categories))
	}
}

func cmdLint(args []string) {
	scope := flagStr(args, "--scope", "default")
	llmMode := flagBool(args, "--llm")
	model := flagStr(args, "--model", defaultModel)
	jsonOut := flagBool(args, "--json")

	var issues []LintIssue

	// Always run structural lint
	issues = append(issues, lintStructural(scope)...)

	// Optionally run LLM lint
	if llmMode {
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			fatal("ANTHROPIC_API_KEY required for --llm lint")
		}
		llmIssues, err := lintLLM(scope, model, apiKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: LLM lint failed: %v\n", err)
		} else {
			issues = append(issues, llmIssues...)
		}
	}

	if jsonOut {
		printJSON(issues)
		return
	}

	if len(issues) == 0 {
		fmt.Println("No issues found. Knowledge base is healthy!")
		return
	}

	fmt.Printf("Found %d issues:\n\n", len(issues))
	for _, issue := range issues {
		icon := map[string]string{"error": "✗", "warning": "!", "info": "·"}[issue.Severity]
		if icon == "" {
			icon = "?"
		}
		fmt.Printf("  [%s] [%s] %s\n", icon, issue.Type, issue.Message)
		if issue.ArticleID != "" {
			fmt.Printf("      Article: %s\n", issue.ArticleID)
		}
		if issue.Suggestion != "" {
			fmt.Printf("      Fix: %s\n", issue.Suggestion)
		}
		fmt.Println()
	}
}

func cmdRecompile(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb recompile <article_id|--all> [--scope NAME] [--model MODEL]")
	}

	scope := flagStr(args, "--scope", "default")
	model := flagStr(args, "--model", defaultModel)
	jsonOut := flagBool(args, "--json")
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	recompileAll := flagBool(args, "--all") || args[0] == "--all"

	var targets []*WikiArticle
	if recompileAll {
		all, _ := listArticles(scope)
		targets = all
	} else {
		a, err := loadArticle(scope, args[0])
		if err != nil || a == nil {
			fatal("Article not found: %s", args[0])
		}
		targets = []*WikiArticle{a}
	}

	var recompiled int
	for _, a := range targets {
		// Load raw source docs
		var texts []string
		for _, docID := range a.SourceDocs {
			raw, err := loadRawDoc(scope, docID)
			if err == nil && raw != nil {
				texts = append(texts, raw.RawText)
			}
		}
		if len(texts) == 0 {
			fmt.Fprintf(os.Stderr, "Warning: no raw docs for %s, skipping\n", a.ID)
			continue
		}

		combined := strings.Join(texts, "\n\n")
		source := "recompile:" + a.ID

		if !jsonOut {
			fmt.Printf("Recompiling: %s\n", a.Title)
		}

		newArticle, _, err := compileLLM(combined, source, model, apiKey, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: recompilation failed for %s: %v\n", a.ID, err)
			continue
		}

		newArticle.ID = a.ID
		newArticle.Version = a.Version + 1
		newArticle.SourceDocs = a.SourceDocs
		saveArticle(scope, newArticle)
		recompiled++
	}

	// Rebuild index
	allArticles, _ := listArticles(scope)
	idx := rebuildIndex(scope, allArticles)
	saveIndex(scope, idx)
	saveSearchIndex(scope, buildSearchIndex(allArticles))

	if jsonOut {
		printJSON(map[string]any{"recompiled": recompiled, "total": len(targets)})
	} else {
		fmt.Printf("Recompiled: %d / %d articles\n", recompiled, len(targets))
	}
}

func cmdClear(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	root := scopeDir(scope)
	os.RemoveAll(root)
	ensureDirs(scope)

	if jsonOut {
		printJSON(map[string]any{"ok": true, "scope": scope})
	} else {
		fmt.Printf("Cleared knowledge base: %s\n", scope)
	}
}

// --- Watch Mode ---

func cmdWatch(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb watch <path> [--scope NAME] [--pattern GLOB] [--model MODEL]")
	}

	path := args[0]
	scope := flagStr(args, "--scope", filepath.Base(path))
	pattern := flagStr(args, "--pattern", "*.py")
	model := flagStr(args, "--model", defaultModel)

	absPath, err := filepath.Abs(path)
	if err != nil {
		fatal("Invalid path: %s", path)
	}

	fmt.Printf("Watching %s (scope: %s, pattern: %s)\n", absPath, scope, pattern)
	fmt.Print("Press Ctrl+C to stop.\n\n")

	// Initial build (non-fatal — dir may be empty initially)
	files := scanDir(absPath, pattern)
	if len(files) > 0 {
		fmt.Println("Running initial build...")
		cmdBuild(append([]string{absPath, "--scope", scope, "--pattern", pattern, "--model", model}))
		fmt.Println()
	} else {
		fmt.Println("No matching files yet. Waiting for changes...")
	}

	watcher, err := newRecursiveWatcher(absPath)
	if err != nil {
		fatal("Failed to create watcher: %v", err)
	}
	defer watcher.Close()

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			matched, _ := filepath.Match(pattern, filepath.Base(event.Name))
			if !matched {
				continue
			}
			// Debounce: wait 3s after last change before rebuilding
			debounce.Reset(3 * time.Second)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "Watcher error: %v\n", err)

		case <-debounce.C:
			fmt.Printf("[%s] Change detected, rebuilding...\n", time.Now().Format("15:04:05"))
			cmdBuild(append([]string{absPath, "--scope", scope, "--pattern", pattern, "--model", model}))
			fmt.Println()
		}
	}
}

func newRecursiveWatcher(root string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "venv": true, ".tox": true, ".mypy_cache": true,
		"dist": true, "build": true, ".eggs": true, ".pytest_cache": true,
	}

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || (strings.HasPrefix(d.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			watcher.Add(path)
		}
		return nil
	})

	return watcher, nil
}

// --- File Scanning ---

func scanDir(root, pattern string) []string {
	var files []string
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "venv": true, ".tox": true, ".mypy_cache": true,
		"dist": true, "build": true, ".eggs": true, ".pytest_cache": true,
	}

	// Support comma-separated patterns: "*.go,*.py,*.ts"
	patterns := strings.Split(pattern, ",")
	for i := range patterns {
		patterns[i] = strings.TrimSpace(patterns[i])
	}

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		for _, p := range patterns {
			if matched, _ := filepath.Match(p, d.Name()); matched {
				files = append(files, path)
				break
			}
		}
		return nil
	})

	sort.Strings(files)
	return files
}

// --- Helpers ---

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

// --- Flag Parsing (minimal, no deps) ---

func flagStr(args []string, name, defaultVal string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return defaultVal
}

func flagBool(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func flagInt(args []string, name string, defaultVal int) int {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			v := 0
			fmt.Sscanf(args[i+1], "%d", &v)
			if v > 0 {
				return v
			}
		}
	}
	return defaultVal
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}

// --- Main ---

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "build":
		cmdBuild(args)
	case "prepare":
		cmdPrepare(args)
	case "accept":
		cmdAccept(args)
	case "search":
		cmdSearch(args)
	case "ingest":
		cmdIngest(args)
	case "show":
		cmdShow(args)
	case "list":
		cmdList(args)
	case "stats":
		cmdStats(args)
	case "lint":
		cmdLint(args)
	case "clear":
		cmdClear(args)
	case "recompile":
		cmdRecompile(args)
	case "watch":
		cmdWatch(args)
	case "version":
		fmt.Println("kb v0.1.0")
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Print(`kb — Headless knowledge base engine

Usage: kb <command> [options]

Commands:
  build <path>           Scan files, compile with LLM, build KB
  prepare <path>         Output compilation prompts as JSON (agent mode, no API key)
  accept                 Read compiled articles from stdin (agent mode companion)
  search <query>         BM25 search over compiled articles
  ingest [file]          Ingest a file or stdin text
  show <article_id>      Show a full article
  list                   List all articles
  stats                  Show KB statistics
  lint                   Structural health check (add --llm for deep check)
  recompile <id|--all>   Force recompile article(s) from raw source
  clear                  Delete all knowledge for a scope
  watch <path>           Auto-rebuild on file changes
  version                Show version

Global flags:
  --scope NAME           Knowledge scope (default: "default")
  --json                 Output as JSON (for machine consumption)
  --model MODEL          LLM model for compilation (default: claude-haiku-4-5-20251001)
  --concurrency N        Parallel LLM compilations (default: 5, build only)
  --pattern GLOB         File patterns, comma-separated (e.g. "*.go,*.py,*.ts")
  --lang LANG            Language hint for stdin ingest (go, python, typescript)

Examples:
  kb build ./src/myapp --scope myapp
  kb search "auth middleware" --scope myapp
  kb ingest ./README.md --scope myapp
  echo "some text" | kb ingest --scope myapp --source "notes"
  kb lint --scope myapp --llm
  kb watch ./src/ --scope myapp --pattern "*.go"

Agent mode (no API key needed):
  kb prepare ./src --scope myapp --pattern "*.go"   # Get prompts
  echo '<compiled JSON>' | kb accept --scope myapp   # Feed results

Environment:
  ANTHROPIC_API_KEY      Required for build/ingest/recompile and --llm lint
                         Not needed for prepare/accept (agent mode)
`)

}
