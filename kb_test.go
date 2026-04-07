// kb_test.go — Tests for the kb knowledge base engine.
// Covers: storage, BM25 search, content hashing, caching, slugify, tokenize,
// frontmatter parsing, index building, structural lint.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- Helpers ---

func tempScope(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Override base path for tests
	scope := "test-" + filepath.Base(dir)
	// Set up scope dir inside temp
	root := filepath.Join(dir, scope)
	os.MkdirAll(filepath.Join(root, "raw"), 0o755)
	os.MkdirAll(filepath.Join(root, "wiki"), 0o755)
	os.MkdirAll(filepath.Join(root, "cache"), 0o755)
	return scope
}

// --- Slugify ---

func TestSlugify(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"Hello World", "hello-world"},
		{"GroupService — manages groups", "groupservice-manages-groups"},
		{"foo/bar/baz.py", "foobarbazpy"},
		{"", ""}, // falls back to hash
		{"UPPER CASE", "upper-case"},
		{"special!@#chars", "specialchars"},
	}
	for _, tt := range tests {
		got := slugify(tt.input)
		if tt.input == "" {
			if len(got) != 16 { // hash fallback
				t.Errorf("slugify(%q) = %q, want 16-char hash", tt.input, got)
			}
			continue
		}
		if got != tt.want {
			t.Errorf("slugify(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSlugifyLongTitle(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := slugify(long)
	if len(got) > 80 {
		t.Errorf("slugify should truncate to 80 chars, got %d", len(got))
	}
}

// --- Tokenize ---

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! This is a TEST-123.")
	expected := []string{"hello", "world", "this", "is", "a", "test", "123"}
	if len(tokens) != len(expected) {
		t.Fatalf("tokenize got %d tokens, want %d: %v", len(tokens), len(expected), tokens)
	}
	for i, tok := range tokens {
		if tok != expected[i] {
			t.Errorf("token[%d] = %q, want %q", i, tok, expected[i])
		}
	}
}

func TestTokenizeEmpty(t *testing.T) {
	tokens := tokenize("")
	if len(tokens) != 0 {
		t.Errorf("tokenize('') should return empty, got %v", tokens)
	}
}

// --- Content Hash ---

func TestContentHash(t *testing.T) {
	h1 := contentHash("hello world")
	h2 := contentHash("hello world")
	h3 := contentHash("hello world!")

	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different input should produce different hash")
	}
	if len(h1) != 64 { // SHA256 hex
		t.Errorf("hash length should be 64, got %d", len(h1))
	}
}

// --- Article Parsing ---

func TestParseArticleWithFrontmatter(t *testing.T) {
	md := `---
{
  "title": "Test Article",
  "summary": "A test summary",
  "concepts": ["go", "testing"],
  "categories": ["code"],
  "source_docs": ["abc123"],
  "backlinks": [],
  "word_count": 5,
  "compiled_at": "2026-04-07T10:00:00Z",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 1
}
---

# Test Content

This is the body.`

	a, err := parseArticle("test-article", md)
	if err != nil {
		t.Fatalf("parseArticle failed: %v", err)
	}

	if a.ID != "test-article" {
		t.Errorf("ID = %q, want %q", a.ID, "test-article")
	}
	if a.Title != "Test Article" {
		t.Errorf("Title = %q, want %q", a.Title, "Test Article")
	}
	if a.Summary != "A test summary" {
		t.Errorf("Summary = %q", a.Summary)
	}
	if len(a.Concepts) != 2 || a.Concepts[0] != "go" {
		t.Errorf("Concepts = %v", a.Concepts)
	}
	if a.CompiledWith != "claude-haiku-4-5-20251001" {
		t.Errorf("CompiledWith = %q", a.CompiledWith)
	}
	if !strings.Contains(a.Content, "# Test Content") {
		t.Errorf("Content should contain body, got %q", a.Content)
	}
}

func TestParseArticleNoFrontmatter(t *testing.T) {
	md := "# Just plain markdown\n\nNo frontmatter here."
	a, err := parseArticle("plain", md)
	if err != nil {
		t.Fatalf("parseArticle failed: %v", err)
	}
	if a.Title != "plain" {
		t.Errorf("Title = %q, want 'plain'", a.Title)
	}
	if a.Content != md {
		t.Errorf("Content should be raw text")
	}
	if a.Version != 1 {
		t.Errorf("Version = %d, want 1", a.Version)
	}
}

func TestParseArticleBadFrontmatter(t *testing.T) {
	md := "---\nnot valid json\n---\n\ncontent"
	_, err := parseArticle("bad", md)
	if err == nil {
		t.Error("expected error for bad frontmatter")
	}
}

// --- Article Storage Round-Trip ---

func TestSaveLoadArticle(t *testing.T) {
	scope := "test-roundtrip-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	original := &WikiArticle{
		ID:           "test-article",
		Title:        "Test Article",
		Summary:      "A test summary",
		Content:      "# Hello\n\nThis is content.",
		Concepts:     []string{"go", "testing"},
		Categories:   []string{"code"},
		SourceDocs:   []string{"raw123"},
		Backlinks:    []string{"other-article"},
		WordCount:    5,
		CompiledAt:   "2026-04-07T10:00:00Z",
		CompiledWith: "test",
		Version:      1,
	}

	err := saveArticle(scope, original)
	if err != nil {
		t.Fatalf("saveArticle failed: %v", err)
	}

	loaded, err := loadArticle(scope, "test-article")
	if err != nil {
		t.Fatalf("loadArticle failed: %v", err)
	}

	if loaded.Title != original.Title {
		t.Errorf("Title = %q, want %q", loaded.Title, original.Title)
	}
	if loaded.Summary != original.Summary {
		t.Errorf("Summary mismatch")
	}
	if loaded.Content != original.Content {
		t.Errorf("Content = %q, want %q", loaded.Content, original.Content)
	}
	if len(loaded.Concepts) != 2 {
		t.Errorf("Concepts = %v", loaded.Concepts)
	}
	if loaded.Version != 1 {
		t.Errorf("Version = %d", loaded.Version)
	}

	os.RemoveAll(scopeDir(scope))
}

// --- RawDoc Storage Round-Trip ---

func TestSaveLoadRawDoc(t *testing.T) {
	scope := "test-raw-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	raw := &RawDoc{
		ID:          "abc123",
		SourceType:  "file",
		Source:      "test.py",
		Filename:    "test.py",
		ContentType: "text",
		RawText:     "print('hello')",
		WordCount:   1,
		IngestedAt:  "2026-04-07T10:00:00Z",
	}

	err := saveRawDoc(scope, raw)
	if err != nil {
		t.Fatalf("saveRawDoc failed: %v", err)
	}

	loaded, err := loadRawDoc(scope, "abc123")
	if err != nil {
		t.Fatalf("loadRawDoc failed: %v", err)
	}
	if loaded.RawText != "print('hello')" {
		t.Errorf("RawText = %q", loaded.RawText)
	}
	if loaded.Source != "test.py" {
		t.Errorf("Source = %q", loaded.Source)
	}
}

// --- Index ---

func TestRebuildIndex(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "a1", Title: "Auth Service", Concepts: []string{"auth", "JWT"}, Categories: []string{"code"}},
		{ID: "a2", Title: "User Service", Concepts: []string{"auth", "users"}, Categories: []string{"code", "api"}},
	}

	idx := rebuildIndex("test", articles)

	if len(idx.Articles) != 2 {
		t.Errorf("Articles count = %d, want 2", len(idx.Articles))
	}

	authConcept := idx.Concepts["auth"]
	if authConcept == nil {
		t.Fatal("auth concept not found")
	}
	if len(authConcept.Articles) != 2 {
		t.Errorf("auth concept has %d articles, want 2", len(authConcept.Articles))
	}

	jwtConcept := idx.Concepts["jwt"]
	if jwtConcept == nil {
		t.Fatal("jwt concept not found")
	}
	if len(jwtConcept.Articles) != 1 {
		t.Errorf("jwt concept has %d articles, want 1", len(jwtConcept.Articles))
	}

	if len(idx.Categories) != 2 {
		t.Errorf("Categories = %v, want [api, code]", idx.Categories)
	}
}

// --- Index Storage Round-Trip ---

func TestSaveLoadIndex(t *testing.T) {
	scope := "test-idx-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	idx := &KnowledgeIndex{
		Scope:    scope,
		Articles: map[string]any{"a1": map[string]any{"title": "Test"}},
		Concepts: map[string]*Concept{"go": {Name: "Go", Articles: []string{"a1"}}},
		Categories: []string{"code"},
	}

	err := saveIndex(scope, idx)
	if err != nil {
		t.Fatalf("saveIndex failed: %v", err)
	}

	loaded := loadIndex(scope)
	if loaded.Scope != scope {
		t.Errorf("Scope = %q", loaded.Scope)
	}
	if len(loaded.Concepts) != 1 {
		t.Errorf("Concepts count = %d", len(loaded.Concepts))
	}
}

// --- BM25 Search ---

func TestBM25Search(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "a1", Title: "Authentication Guide", Summary: "How to authenticate users with JWT tokens", Content: "JWT auth flow using bearer tokens"},
		{ID: "a2", Title: "Database Setup", Summary: "Setting up PostgreSQL for production", Content: "PostgreSQL configuration and connection pooling"},
		{ID: "a3", Title: "API Gateway", Summary: "Gateway handles auth and routing", Content: "Routes requests and validates JWT tokens"},
	}

	results := bm25Search(articles, "JWT authentication", 5)
	if len(results) == 0 {
		t.Fatal("expected results for 'JWT authentication'")
	}
	// Auth guide should rank first (has both JWT and auth)
	if results[0].ID != "a1" {
		t.Errorf("expected a1 first, got %s", results[0].ID)
	}
}

func TestBM25SearchNoResults(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "a1", Title: "Hello", Content: "world"},
	}
	results := bm25Search(articles, "nonexistent", 5)
	if len(results) != 0 {
		t.Errorf("expected no results, got %d", len(results))
	}
}

func TestBM25SearchEmptyQuery(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "a1", Title: "Hello", Content: "world"},
	}
	results := bm25Search(articles, "", 5)
	if results != nil {
		t.Errorf("expected nil for empty query, got %v", results)
	}
}

func TestBM25SearchEmptyCorpus(t *testing.T) {
	results := bm25Search(nil, "test", 5)
	if results != nil {
		t.Errorf("expected nil for empty corpus")
	}
}

func TestBM25SearchLimit(t *testing.T) {
	articles := []*WikiArticle{
		{ID: "a1", Title: "Go", Summary: "Go language", Content: "Go programming language"},
		{ID: "a2", Title: "Go testing", Summary: "Go tests", Content: "Go test framework"},
		{ID: "a3", Title: "Go modules", Summary: "Go mod", Content: "Go module system"},
	}
	results := bm25Search(articles, "Go", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

// --- Cache ---

func TestCacheRoundTrip(t *testing.T) {
	scope := "test-cache-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	c := &Cache{
		Version: 1,
		Files: map[string]CacheEntry{
			"src/main.py": {Hash: "abc123", ArticleID: "main-py", CompiledAt: "2026-04-07T10:00:00Z"},
		},
	}

	ensureDirs(scope)
	err := saveCache(scope, c)
	if err != nil {
		t.Fatalf("saveCache failed: %v", err)
	}

	loaded := loadCache(scope)
	if loaded.Version != 1 {
		t.Errorf("Version = %d", loaded.Version)
	}
	entry, ok := loaded.Files["src/main.py"]
	if !ok {
		t.Fatal("cache entry not found")
	}
	if entry.Hash != "abc123" {
		t.Errorf("Hash = %q", entry.Hash)
	}
}

func TestCacheHit(t *testing.T) {
	text := "print('hello world')"
	hash := contentHash(text)

	cache := &Cache{
		Version: 1,
		Files: map[string]CacheEntry{
			"test.py": {Hash: hash, ArticleID: "test-py"},
		},
	}

	entry, ok := cache.Files["test.py"]
	if !ok || entry.Hash != hash {
		t.Error("expected cache hit for same content")
	}
}

func TestCacheMiss(t *testing.T) {
	cache := &Cache{
		Version: 1,
		Files: map[string]CacheEntry{
			"test.py": {Hash: "old-hash", ArticleID: "test-py"},
		},
	}

	newHash := contentHash("modified content")
	entry := cache.Files["test.py"]
	if entry.Hash == newHash {
		t.Error("expected cache miss for changed content")
	}
}

// --- Structural Lint ---

func TestLintEmptyKB(t *testing.T) {
	scope := "test-lint-empty-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()
	ensureDirs(scope)

	issues := lintStructural(scope)
	if len(issues) == 0 {
		t.Error("expected warning for empty KB")
	}
	if issues[0].Type != "gap" {
		t.Errorf("expected 'gap' issue, got %q", issues[0].Type)
	}
}

func TestLintMissingConcepts(t *testing.T) {
	scope := "test-lint-concepts-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	article := &WikiArticle{
		ID:       "test",
		Title:    "Test",
		Summary:  "A test",
		Content:  "Some content",
		Concepts: []string{}, // empty
		Version:  1,
	}
	saveArticle(scope, article)

	issues := lintStructural(scope)
	found := false
	for _, issue := range issues {
		if issue.Type == "gap" && strings.Contains(issue.Message, "no concepts") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about missing concepts")
	}
}

func TestLintBrokenBacklink(t *testing.T) {
	scope := "test-lint-backlink-" + contentHash(t.Name())[:8]
	defer func() { os.RemoveAll(scopeDir(scope)) }()

	article := &WikiArticle{
		ID:        "test",
		Title:     "Test",
		Summary:   "A test",
		Content:   "Content",
		Concepts:  []string{"test"},
		Backlinks: []string{"nonexistent"},
		Version:   1,
	}
	saveArticle(scope, article)

	issues := lintStructural(scope)
	found := false
	for _, issue := range issues {
		if issue.Type == "connection" && strings.Contains(issue.Message, "broken backlink") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about broken backlink")
	}
}

// --- File Scanning ---

func TestScanDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "main.py"), []byte("# main"), 0o644)
	os.WriteFile(filepath.Join(dir, "test.py"), []byte("# test"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# readme"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("git"), 0o644)

	files := scanDir(dir, "*.py")
	if len(files) != 2 {
		t.Errorf("expected 2 .py files, got %d: %v", len(files), files)
	}

	// Should skip .git
	allFiles := scanDir(dir, "*")
	for _, f := range allFiles {
		if strings.Contains(f, ".git") {
			t.Error("scanDir should skip .git directory")
		}
	}
}

func TestScanDirSkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.js"), []byte("//"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("// app"), 0o644)

	files := scanDir(dir, "*.js")
	if len(files) != 1 {
		t.Errorf("expected 1 file (skipping node_modules), got %d: %v", len(files), files)
	}
}

// --- Helpers ---

func TestWordCount(t *testing.T) {
	if wordCount("hello world") != 2 {
		t.Error("wordCount('hello world') != 2")
	}
	if wordCount("") != 0 {
		t.Error("wordCount('') != 0")
	}
	if wordCount("  spaced  out  ") != 2 {
		t.Error("wordCount with extra spaces")
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("short string should not be truncated")
	}
	result := truncate("hello world foo bar", 10)
	if len(result) > 13 { // 10 + "..."
		t.Errorf("truncated string too long: %q", result)
	}
	if !strings.HasSuffix(result, "...") {
		t.Errorf("should end with ...: %q", result)
	}
}

func TestNilToEmpty(t *testing.T) {
	var nilSlice []string
	result := nilToEmpty(nilSlice)
	if result == nil {
		t.Error("nilToEmpty should return empty slice, not nil")
	}
	if len(result) != 0 {
		t.Error("nilToEmpty should return empty slice")
	}

	existing := []string{"a", "b"}
	result2 := nilToEmpty(existing)
	if len(result2) != 2 {
		t.Error("nilToEmpty should preserve existing slice")
	}
}

// --- Go AST Parser ---

func TestParseGoBasic(t *testing.T) {
	source := `package main

import (
	"fmt"
	"net/http"
)

// Server handles HTTP requests.
type Server struct {
	Port int
	Host string
}

// Start boots the server.
func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {}

// HealthCheck is a standalone function.
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, "ok")
}

type Handler interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

const MaxRetries = 3
`
	mod := parseGo("main.go", source)
	if mod == nil {
		t.Fatal("parseGo returned nil")
	}
	if mod.Language != "go" {
		t.Errorf("Language = %q", mod.Language)
	}
	if mod.Package != "main" {
		t.Errorf("Package = %q", mod.Package)
	}
	if len(mod.Imports) != 2 {
		t.Errorf("Imports = %v", mod.Imports)
	}

	// Should have Server struct, Handler interface
	if len(mod.Types) < 2 {
		t.Fatalf("Types count = %d, want >= 2", len(mod.Types))
	}

	// Find Server struct
	var server *CodeType
	for i, typ := range mod.Types {
		if typ.Name == "Server" {
			server = &mod.Types[i]
			break
		}
	}
	if server == nil {
		t.Fatal("Server struct not found")
	}
	if server.Kind != "struct" {
		t.Errorf("Server.Kind = %q", server.Kind)
	}
	if len(server.Fields) != 2 {
		t.Errorf("Server.Fields = %v", server.Fields)
	}
	if len(server.Methods) != 2 {
		t.Errorf("Server should have 2 methods (Start, Stop), got %d", len(server.Methods))
	}

	// Find Handler interface
	var handler *CodeType
	for i, typ := range mod.Types {
		if typ.Name == "Handler" {
			handler = &mod.Types[i]
		}
	}
	if handler == nil {
		t.Fatal("Handler interface not found")
	}
	if handler.Kind != "interface" {
		t.Errorf("Handler.Kind = %q", handler.Kind)
	}

	// HealthCheck should be a top-level function
	if len(mod.Functions) < 1 {
		t.Fatal("expected at least 1 top-level function")
	}
	if mod.Functions[0].Name != "HealthCheck" {
		t.Errorf("Function name = %q", mod.Functions[0].Name)
	}
}

func TestParseGoInvalidSyntax(t *testing.T) {
	mod := parseGo("bad.go", "this is not go code {{{")
	if mod != nil {
		t.Error("expected nil for invalid Go")
	}
}

// --- Python AST Parser ---

func TestParsePythonBasic(t *testing.T) {
	source := `"""Module docstring."""

import os
from pathlib import Path
from typing import Optional

MAX_RETRIES = 3
DEFAULT_TIMEOUT = 30

class UserService(BaseService):
    """Handles user operations."""

    async def create_user(self, name: str, email: str) -> dict:
        pass

    def get_user(self, user_id: int) -> Optional[dict]:
        pass

class AdminService(UserService):
    pass

def health_check(request) -> str:
    """Check system health."""
    return "ok"

async def process_batch(items: list, limit: int = 10):
    pass

def _private_helper():
    pass
`
	mod := parsePython("service.py", source)
	if mod == nil {
		t.Fatal("parsePython returned nil")
	}
	if mod.Language != "python" {
		t.Errorf("Language = %q", mod.Language)
	}
	if mod.Docstring != "Module docstring." {
		t.Errorf("Docstring = %q", mod.Docstring)
	}

	// Imports
	if len(mod.Imports) < 3 {
		t.Errorf("Imports = %v", mod.Imports)
	}

	// Constants
	if len(mod.Constants) < 2 {
		t.Errorf("Constants = %v, want at least MAX_RETRIES and DEFAULT_TIMEOUT", mod.Constants)
	}

	// Classes
	if len(mod.Types) != 2 {
		t.Fatalf("Types count = %d, want 2", len(mod.Types))
	}
	if mod.Types[0].Name != "UserService" {
		t.Errorf("Types[0].Name = %q", mod.Types[0].Name)
	}
	if mod.Types[0].Kind != "class" {
		t.Errorf("Types[0].Kind = %q", mod.Types[0].Kind)
	}
	if len(mod.Types[0].Bases) == 0 || mod.Types[0].Bases[0] != "BaseService" {
		t.Errorf("Types[0].Bases = %v", mod.Types[0].Bases)
	}
	if mod.Types[0].Docstring != "Handles user operations." {
		t.Errorf("Types[0].Docstring = %q", mod.Types[0].Docstring)
	}

	// Methods
	if len(mod.Types[0].Methods) != 2 {
		t.Errorf("UserService methods = %d, want 2", len(mod.Types[0].Methods))
	}
	if len(mod.Types[0].Methods) > 0 && !mod.Types[0].Methods[0].IsAsync {
		t.Error("create_user should be async")
	}

	// Top-level functions
	if len(mod.Functions) < 2 {
		t.Fatalf("Functions = %d, want >= 2", len(mod.Functions))
	}
}

// --- TypeScript AST Parser ---

func TestParseTypeScriptBasic(t *testing.T) {
	source := `import { Request, Response } from 'express'
import { UserModel } from './models/user'
import jwt from 'jsonwebtoken'

export interface AuthProvider {
	authenticate(token: string): Promise<User>
}

export class AuthService extends BaseService implements AuthProvider {
	async authenticate(token: string): Promise<User> {
		return jwt.verify(token)
	}
}

export type UserId = string

export enum Role {
	Admin,
	User,
	Guest,
}

export async function createUser(name: string, email: string): Promise<User> {
	return new User(name, email)
}

export const handleRequest = async (req: Request, res: Response) => {
	res.json({ ok: true })
}

function internalHelper(x: number): boolean {
	return x > 0
}
`
	mod := parseTypeScript("auth.ts", source, "typescript")
	if mod == nil {
		t.Fatal("parseTypeScript returned nil")
	}
	if mod.Language != "typescript" {
		t.Errorf("Language = %q", mod.Language)
	}

	// Imports
	if len(mod.Imports) != 3 {
		t.Errorf("Imports = %v, want 3", mod.Imports)
	}

	// Types: AuthProvider (interface), AuthService (class), UserId (type), Role (enum)
	if len(mod.Types) != 4 {
		t.Fatalf("Types = %d, want 4: %+v", len(mod.Types), mod.Types)
	}

	// Check interface
	found := false
	for _, typ := range mod.Types {
		if typ.Name == "AuthProvider" && typ.Kind == "interface" {
			found = true
		}
	}
	if !found {
		t.Error("AuthProvider interface not found")
	}

	// Check class with extends + implements
	for _, typ := range mod.Types {
		if typ.Name == "AuthService" {
			if typ.Kind != "class" {
				t.Errorf("AuthService.Kind = %q", typ.Kind)
			}
			if len(typ.Bases) < 2 {
				t.Errorf("AuthService.Bases = %v, want BaseService + AuthProvider", typ.Bases)
			}
			if !typ.IsExported {
				t.Error("AuthService should be exported")
			}
		}
	}

	// Functions: createUser, handleRequest (arrow), internalHelper
	if len(mod.Functions) < 2 {
		t.Errorf("Functions = %d, want >= 2", len(mod.Functions))
	}
}

// --- formatCodeContext ---

func TestFormatCodeContext(t *testing.T) {
	mod := &CodeModule{
		Language: "go",
		Package:  "main",
		Imports:  []string{"fmt", "net/http"},
		Types: []CodeType{
			{Name: "Server", Kind: "struct", Fields: []string{"Port", "Host"}, IsExported: true},
		},
		Functions: []CodeFunc{
			{Name: "main", Args: nil, IsExported: false},
		},
	}

	output := formatCodeContext(mod)
	if !strings.Contains(output, "Language: go") {
		t.Error("missing language")
	}
	if !strings.Contains(output, "Package: main") {
		t.Error("missing package")
	}
	if !strings.Contains(output, "struct Server") {
		t.Error("missing struct Server")
	}
	if !strings.Contains(output, "main()") {
		t.Error("missing main function")
	}
}

// --- detectLanguage ---

func TestDetectLanguage(t *testing.T) {
	tests := map[string]string{
		"main.go":       "go",
		"service.py":    "python",
		"app.ts":        "typescript",
		"component.tsx": "typescript",
		"index.js":      "javascript",
		"readme.md":     "",
		"data.json":     "",
	}
	for path, want := range tests {
		got := detectLanguage(path)
		if got != want {
			t.Errorf("detectLanguage(%q) = %q, want %q", path, got, want)
		}
	}
}

// --- Format Compatibility (Python ↔ Go) ---

func TestPythonFormatCompatibility(t *testing.T) {
	// This is the exact format that the Python knowledge-base package writes.
	// Go must be able to read it.
	pythonOutput := `---
{
  "title": "GroupService",
  "summary": "Handles group CRUD operations",
  "concepts": ["GroupService", "membership", "Beanie ODM"],
  "categories": ["code"],
  "source_docs": ["abc123def456"],
  "backlinks": ["message_service"],
  "word_count": 450,
  "compiled_at": "2026-04-06T18:00:00+00:00",
  "compiled_with": "claude-haiku-4-5-20251001",
  "version": 2
}
---

# GroupService

Handles group creation, membership, and settings.

## Classes

### GroupService(BaseService)

Main service for group operations.`

	a, err := parseArticle("group_service", pythonOutput)
	if err != nil {
		t.Fatalf("Failed to parse Python-format article: %v", err)
	}

	if a.Title != "GroupService" {
		t.Errorf("Title = %q", a.Title)
	}
	if len(a.Concepts) != 3 {
		t.Errorf("Concepts = %v", a.Concepts)
	}
	if a.Version != 2 {
		t.Errorf("Version = %d", a.Version)
	}
	if !strings.Contains(a.Content, "# GroupService") {
		t.Error("Content missing body")
	}
	if a.Backlinks[0] != "message_service" {
		t.Errorf("Backlinks = %v", a.Backlinks)
	}
}
