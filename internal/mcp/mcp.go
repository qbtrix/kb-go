// Package mcp is kb's read-only MCP (Model Context Protocol) server.
//
// Exposes the existing knowledge-base read paths over a hand-rolled JSON-RPC
// 2.0 server on stdio, so an in-loop agent can query the index without
// shelling out to `kb` once per question. No external MCP library: the binary
// stays zero-dependency. The server implements three MCP methods —
// initialize, tools/list, tools/call — and registers five read-only tools:
//
//	kb_search   — BM25 (+ optional vector/hybrid) retrieval, same path as `kb search --json`
//	kb_show     — fetch one article by id, same shape as `kb show --json`
//	kb_glossary — resolve a term to its concept and the articles that define it
//	kb_stats    — index overview, same shape as `kb stats --json`
//	kb_list     — list articles, same shape as `kb list --json`
//
// Every handler calls the same underlying primitives the CLI uses and returns
// the identical JSON the `--json` CLI flag emits — parity by construction. No
// build/ingest/mutation tools are exposed; serving is read-only by design.
//
// The server is long-lived while other processes write the same scopes, so
// kb_search / kb_list / kb_stats read articles through articleCache instead
// of re-parsing every wiki file per call. Invariant: every call re-stats the
// scope (one wiki ReadDir; mtime+size come from the directory entries) and
// re-parses only files that were added or whose stamp changed; vanished files
// are dropped. A file read less than racyWindow after its mtime is never
// trusted (an overwrite inside one filesystem clock tick can keep both mtime
// and size), so it is re-read on every call until it settles. The search
// index file is cached under the same stamp rule, and the cached article
// slice keeps store.ListArticles' ID order so search.Index docIdx stays aligned
// (search.HealIndex still rebuilds whenever ids/order drift). kb_show and
// kb_glossary read single files and stay uncached.
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/search"
	"github.com/qbtrix/kb-go/internal/store"
	"github.com/qbtrix/kb-go/internal/textutil"
)

// --- JSON-RPC 2.0 wire types ---

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"` // may be number, string, or absent (notification)
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// JSON-RPC standard error codes.
const (
	errParse          = -32700
	errInvalidRequest = -32600
	errMethodNotFound = -32601
	errInvalidParams  = -32602
	errInternal       = -32603
)

const mcpProtocolVersion = "2024-11-05"

// --- MCP tool schema types ---

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolHandler runs a tool and returns a JSON-serializable payload. A returned
// error becomes an MCP tool error (isError: true) rather than a transport
// error — the agent sees the message instead of the connection dropping.
type toolHandler func(args map[string]any) (any, error)

// Server holds the registered tools and the stdio transport.
type Server struct {
	in    io.Reader
	out   io.Writer
	tools []mcpTool
	funcs map[string]toolHandler
}

// --- Entry point (wired into main's dispatch as `case "serve"`) ---

func NewServer(in io.Reader, out io.Writer, defaultScope string) *Server {
	s := &Server{in: in, out: out, funcs: map[string]toolHandler{}}
	registerKBTools(s, defaultScope)
	return s
}

// register adds a tool and its handler to the server.
func (s *Server) register(t mcpTool, h toolHandler) {
	s.tools = append(s.tools, t)
	s.funcs[t.Name] = h
}

// --- Transport loop: one JSON-RPC message per line over stdio ---

func (s *Server) Serve() error {
	scanner := bufio.NewScanner(s.in)
	// Articles can be large; allow long lines for tools/call payloads.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		s.handleLine([]byte(line))
	}
	return scanner.Err()
}

func (s *Server) handleLine(line []byte) {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		s.writeError(nil, errParse, "parse error", err.Error())
		return
	}
	// Notifications (no id) get processed but never answered, per JSON-RPC.
	isNotification := len(req.ID) == 0

	switch req.Method {
	case "initialize":
		if isNotification {
			return
		}
		s.writeResult(req.ID, map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "kb-go",
				"version": "0.1.0",
			},
		})
	case "notifications/initialized", "initialized":
		// Client handshake ack — nothing to answer.
		return
	case "tools/list":
		if isNotification {
			return
		}
		s.writeResult(req.ID, map[string]any{"tools": s.tools})
	case "tools/call":
		if isNotification {
			return
		}
		s.handleToolCall(req)
	case "ping":
		if isNotification {
			return
		}
		s.writeResult(req.ID, map[string]any{})
	default:
		if isNotification {
			return
		}
		s.writeError(req.ID, errMethodNotFound, "method not found", req.Method)
	}
}

func (s *Server) handleToolCall(req rpcRequest) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.writeError(req.ID, errInvalidParams, "invalid params", err.Error())
		return
	}
	h, ok := s.funcs[params.Name]
	if !ok {
		s.writeError(req.ID, errMethodNotFound, "unknown tool", params.Name)
		return
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	payload, err := h(params.Arguments)
	if err != nil {
		// Tool-level failure: report via MCP content with isError, so the
		// agent gets the message rather than a dropped JSON-RPC error.
		s.writeResult(req.ID, toolError(err.Error()))
		return
	}
	s.writeResult(req.ID, toolJSON(payload))
}

// --- MCP tools/call result helpers ---

// toolJSON wraps a payload as an MCP text-content result whose body is the
// machine-readable JSON (never the Rich/table human form).
func toolJSON(payload any) map[string]any {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return toolError(fmt.Sprintf("marshal result: %v", err))
	}
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": string(data)},
		},
		"isError": false,
	}
}

func toolError(msg string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": msg},
		},
		"isError": true,
	}
}

// --- Response writers ---

func (s *Server) writeResult(id json.RawMessage, result any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *Server) writeError(id json.RawMessage, code int, msg string, data any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}})
}

func (s *Server) write(resp rpcResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		// Last-ditch: can't even marshal the error. Emit a static parse-fail.
		fmt.Fprintln(s.out, `{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"internal error"}}`)
		return
	}
	fmt.Fprintln(s.out, string(data))
}

// --- argument coercion helpers (JSON numbers decode as float64) ---

func argStr(args map[string]any, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func argInt(args map[string]any, key string, def int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case string:
			parsed := 0
			if _, err := fmt.Sscanf(n, "%d", &parsed); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return def
}

func argBool(args map[string]any, key string, def bool) bool {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// --- Tool registration: wraps the existing read paths ---

func registerKBTools(s *Server, defaultScope string) {
	cache := newArticleCache()
	scopeProp := map[string]any{
		"type":        "string",
		"description": fmt.Sprintf("Knowledge scope to query. '*' or 'a,b' for multi-scope. Defaults to %q.", defaultScope),
	}

	// kb_search — mirrors `cmdSearch` --json output exactly.
	s.register(mcpTool{
		Name:        "kb_search",
		Description: "Search the knowledge base. BM25 by default; vector or hybrid when a query embedding is supplied. Returns ranked articles (id, title, summary, concepts). Same retrieval path as `kb search`.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":        map[string]any{"type": "string", "description": "Text query. Required unless query_vec_path is set for pure vector search."},
				"scope":        scopeProp,
				"limit":        map[string]any{"type": "integer", "description": "Max results (default 5)."},
				"exclude_tags": map[string]any{"type": "string", "description": "Comma-separated category tags to drop from results."},
				"query_vec_path": map[string]any{
					"type":        "string",
					"description": "Path to a JSON file holding a query embedding. Triggers vector search. Single scope only.",
				},
				"hybrid": map[string]any{"type": "boolean", "description": "Combine BM25 + vector. Requires both query and query_vec_path."},
				"topk":   map[string]any{"type": "integer", "description": "Vector/hybrid top-K (defaults to limit)."},
			},
		},
	}, func(args map[string]any) (any, error) {
		return mcpSearch(cache, args, defaultScope)
	})

	// kb_show — mirrors `cmdShow` --json output exactly.
	s.register(mcpTool{
		Name:        "kb_show",
		Description: "Fetch one full article by id, including its compiled content. Same as `kb show <id>`.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id":    map[string]any{"type": "string", "description": "Article id."},
				"scope": scopeProp,
			},
			"required": []string{"id"},
		},
	}, func(args map[string]any) (any, error) {
		return mcpShow(args, defaultScope)
	})

	// kb_glossary — resolve a term to its concept + defining articles.
	s.register(mcpTool{
		Name:        "kb_glossary",
		Description: "Resolve a term to its canonical concept and the articles that define it. Looks up the concept index built during compilation.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"term":  map[string]any{"type": "string", "description": "Term to resolve (case-insensitive)."},
				"scope": scopeProp,
			},
			"required": []string{"term"},
		},
	}, func(args map[string]any) (any, error) {
		return mcpGlossary(args, defaultScope)
	})

	// kb_stats — mirrors `cmdStats` --json output exactly.
	s.register(mcpTool{
		Name:        "kb_stats",
		Description: "Index overview for a scope: article/raw/word/concept/category/vector counts. Same as `kb stats`.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"scope": scopeProp},
		},
	}, func(args map[string]any) (any, error) {
		return mcpStats(cache, args, defaultScope)
	})

	// kb_list — mirrors `cmdList` --json output exactly.
	s.register(mcpTool{
		Name:        "kb_list",
		Description: "List all articles in a scope (id, title, summary, word_count, version). Same as `kb list`.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"scope": scopeProp},
		},
	}, func(args map[string]any) (any, error) {
		return mcpList(cache, args, defaultScope)
	})
}

// --- Tool handlers: reuse CLI primitives, return the CLI's --json shapes ---

// mcpSearch replicates cmdSearch's --json branch using the same primitives.
// Returns []map (BM25 path) or vector results, matching the CLI byte shape.
func mcpSearch(c *articleCache, args map[string]any, defaultScope string) (any, error) {
	query := argStr(args, "query", "")
	scope := argStr(args, "scope", defaultScope)
	limit := argInt(args, "limit", 5)
	excludeTags := argStr(args, "exclude_tags", "")
	queryVecPath := argStr(args, "query_vec_path", "")
	hybridMode := argBool(args, "hybrid", false)
	topK := argInt(args, "topk", limit)

	// Vector / hybrid path — same routing as cmdSearch.
	if queryVecPath != "" {
		if scope == "*" || strings.Contains(scope, ",") {
			return nil, fmt.Errorf("vector search requires a single scope, got %q", scope)
		}
		// Agent-controlled path over a persistent connection — contain it to
		// the kb base dir so it can't be used as a read-oracle (issue #23).
		queryVec, err := store.LoadContainedVector(queryVecPath)
		if err != nil {
			return nil, fmt.Errorf("load query vector: %v", err)
		}
		var results []search.Hit
		if hybridMode {
			if query == "" {
				return nil, fmt.Errorf("hybrid requires a text query alongside query_vec_path")
			}
			results, err = search.HybridSearch(scope, query, queryVec, topK)
		} else {
			results, err = search.VectorSearch(scope, queryVec, topK)
		}
		if err != nil {
			return nil, err
		}
		return results, nil
	}

	scopes := store.ResolveScopes(scope)

	var allArticles []*model.WikiArticle
	var scopeMap []string
	for _, sc := range scopes {
		articles, err := c.list(sc)
		if err != nil {
			continue
		}
		for _, a := range articles {
			allArticles = append(allArticles, a)
			scopeMap = append(scopeMap, sc)
		}
	}

	if excludeTags != "" {
		excluded := strings.Split(excludeTags, ",")
		var filtered []*model.WikiArticle
		var filteredScopes []string
		for i, a := range allArticles {
			skip := false
			for _, tag := range excluded {
				tag = strings.TrimSpace(tag)
				if slices.Contains(a.Categories, tag) {
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

	var results []*model.WikiArticle
	if len(scopes) == 1 {
		var si *search.Index
		if excludeTags == "" {
			// Full-scope search: self-heal a missing/stale/old-format index
			// (best-effort cache write; failure never fails the search) so
			// long-lived read-only servers regain the fast path.
			si = search.HealIndex(scopes[0], allArticles, c.searchIndex(scopes[0]))
		} else {
			// Tag-filtered slice — full-scope index can't match; slow path.
			si = c.searchIndex(scopes[0])
		}
		results = search.BM25WithIndex(allArticles, query, limit, si)
	} else {
		results = search.BM25(allArticles, query, limit)
	}

	resultScope := func(a *model.WikiArticle) string {
		for i, art := range allArticles {
			if art == a && i < len(scopeMap) {
				return scopeMap[i]
			}
		}
		return ""
	}
	multiScope := len(scopes) > 1

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
	return out, nil
}

// mcpShow replicates cmdShow's --json branch.
func mcpShow(args map[string]any, defaultScope string) (any, error) {
	id := argStr(args, "id", "")
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}
	scope := argStr(args, "scope", defaultScope)

	a, err := store.LoadArticle(scope, id)
	if err != nil || a == nil {
		return nil, fmt.Errorf("article not found: %s", id)
	}
	return map[string]any{
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
	}, nil
}

// mcpGlossary resolves a term against the compiled concept index. Concepts are
// keyed lowercase in index.json (see store.RebuildIndex); the linked articles' titles
// and summaries are the canonical definition surface kb-go holds. Read-only.
func mcpGlossary(args map[string]any, defaultScope string) (any, error) {
	term := argStr(args, "term", "")
	if term == "" {
		return nil, fmt.Errorf("term is required")
	}
	scope := argStr(args, "scope", defaultScope)

	idx := store.LoadIndex(scope)
	key := strings.ToLower(strings.TrimSpace(term))
	concept, ok := idx.Concepts[key]
	if !ok || concept == nil {
		return map[string]any{
			"term":     term,
			"scope":    scope,
			"resolved": false,
		}, nil
	}

	defs := make([]map[string]any, 0, len(concept.Articles))
	for _, aid := range concept.Articles {
		a, err := store.LoadArticle(scope, aid)
		if err != nil || a == nil {
			continue
		}
		defs = append(defs, map[string]any{
			"id":      a.ID,
			"title":   a.Title,
			"summary": a.Summary,
		})
	}
	return map[string]any{
		"term":          term,
		"scope":         scope,
		"resolved":      true,
		"canonical":     concept.Name,
		"category":      concept.Category,
		"article_count": len(concept.Articles),
		"definitions":   defs,
	}, nil
}

// mcpStats replicates cmdStats's --json branch.
func mcpStats(c *articleCache, args map[string]any, defaultScope string) (any, error) {
	scope := argStr(args, "scope", defaultScope)

	articles, _ := c.list(scope)
	idx := store.LoadIndex(scope)
	rawCount := 0
	if entries, err := os.ReadDir(filepath.Join(store.ScopeDir(scope), "raw")); err == nil {
		rawCount = len(entries)
	}
	totalWords := 0
	for _, a := range articles {
		totalWords += a.WordCount
	}
	return map[string]any{
		"scope":      scope,
		"articles":   len(articles),
		"raw_docs":   rawCount,
		"words":      totalWords,
		"concepts":   len(idx.Concepts),
		"categories": len(idx.Categories),
		"vectors":    store.VectorCount(scope),
	}, nil
}

// mcpList replicates cmdList's --json branch.
func mcpList(c *articleCache, args map[string]any, defaultScope string) (any, error) {
	scope := argStr(args, "scope", defaultScope)

	articles, _ := c.list(scope)
	out := make([]map[string]any, 0, len(articles))
	for _, a := range articles {
		out = append(out, map[string]any{
			"id":            a.ID,
			"title":         a.Title,
			"summary":       textutil.Truncate(a.Summary, 120),
			"word_count":    a.WordCount,
			"compiled_with": a.CompiledWith,
			"version":       a.Version,
		})
	}
	return out, nil
}

// --- Article cache: re-stat every call, re-parse only what changed ---

// racyWindow is how long after its mtime a file read stays untrusted. A later
// write in the same filesystem clock tick (~1-16 ms on NTFS, 2 s on FAT) can
// leave mtime AND size unchanged, so a read that close to the mtime cannot
// prove that a future identical stamp means identical content. Once a file's
// mtime is older than the window at read time, any later write gets a later
// mtime. Assumes writers share this machine's clock (local filesystem).
const racyWindow = 3 * time.Second

// fileStamp is the change signal for one file: mtime + size.
type fileStamp struct {
	mod  time.Time
	size int64
}

func (a fileStamp) same(b fileStamp) bool { return a.size == b.size && a.mod.Equal(b.mod) }

func stampOf(info os.FileInfo) fileStamp { return fileStamp{info.ModTime(), info.Size()} }

type cachedArticle struct {
	stamp   fileStamp
	trusted bool // read at least racyWindow after its mtime
	article *model.WikiArticle
}

// scopeCache holds one scope's parsed articles and its loaded search index.
type scopeCache struct {
	files    map[string]cachedArticle // article id -> parsed file
	articles []*model.WikiArticle     // assembled slice, listArticles (ID) order
	built    bool

	si        *search.Index
	siStamp   fileStamp
	siTrusted bool
}

// articleCache is the MCP server's per-scope article/search-index cache. It
// never serves a file without first re-statting it in the same call, so
// writes by other processes are visible on the very next call.
type articleCache struct {
	mu     sync.Mutex
	scopes map[string]*scopeCache // keyed by scopeDir (sanitized names collide)
}

func newArticleCache() *articleCache {
	return &articleCache{scopes: map[string]*scopeCache{}}
}

// list returns the scope's articles exactly as store.ListArticles would read them
// from disk right now. The returned slice and articles are shared: read-only.
func (c *articleCache) list(scope string) ([]*model.WikiArticle, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := store.ScopeDir(scope)
	dir := filepath.Join(key, "wiki")
	// Taken before any stat: a write this call did not observe happens later.
	now := time.Now()
	entries, err := os.ReadDir(dir)
	if err != nil {
		delete(c.scopes, key)
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sc := c.scopes[key]
	if sc == nil {
		sc = &scopeCache{files: map[string]cachedArticle{}}
		c.scopes[key] = sc
	}

	changed := !sc.built
	seen := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), ".md")
		info, err := e.Info()
		if err != nil {
			continue // removed since ReadDir
		}
		st := stampOf(info)
		if ca, ok := sc.files[id]; ok && ca.trusted && ca.stamp.same(st) {
			seen[id] = true
			continue
		}
		// Stamp first, then read: if the file changes in between, the next
		// call sees a newer stamp and re-reads.
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		a, err := store.ParseArticle(id, string(data))
		if err != nil {
			continue // same skip rule as listArticles
		}
		sc.files[id] = cachedArticle{stamp: st, trusted: now.Sub(st.mod) >= racyWindow, article: a}
		seen[id] = true
		changed = true
	}
	for id := range sc.files {
		if !seen[id] {
			delete(sc.files, id)
			changed = true
		}
	}

	if changed {
		articles := make([]*model.WikiArticle, 0, len(sc.files))
		for _, ca := range sc.files {
			articles = append(articles, ca.article)
		}
		sort.Slice(articles, func(i, j int) bool { return articles[i].ID < articles[j].ID })
		sc.articles = articles
		sc.built = true
	}
	return sc.articles, nil
}

// searchIndex returns what search.LoadIndex would return right now, reusing the
// decoded index while cache/search_index.json keeps a trusted stamp.
func (c *articleCache) searchIndex(scope string) *search.Index {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := store.ScopeDir(scope)
	now := time.Now()
	info, err := os.Stat(filepath.Join(key, "cache", "search_index.json"))
	sc := c.scopes[key]
	if err != nil {
		if sc != nil {
			sc.si, sc.siTrusted = nil, false
		}
		return nil
	}
	st := stampOf(info)
	if sc != nil && sc.siTrusted && sc.siStamp.same(st) {
		return sc.si
	}
	si := search.LoadIndex(scope)
	if sc != nil {
		sc.si, sc.siStamp, sc.siTrusted = si, st, now.Sub(st.mod) >= racyWindow
	}
	return si
}
