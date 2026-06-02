// mcp.go — Read-only MCP (Model Context Protocol) server for kb-go.
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
// Every handler calls the same underlying primitives the CLI uses
// (listArticles, loadSearchIndex/bm25SearchWithIndex, loadArticle, loadIndex,
// runVectorSearch/runHybridSearch) and returns the identical JSON the
// `--json` CLI flag emits — parity by construction. No build/ingest/mutation
// tools are exposed; serving is read-only by design.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
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

// mcpServer holds the registered tools and the stdio transport.
type mcpServer struct {
	in    io.Reader
	out   io.Writer
	tools []mcpTool
	funcs map[string]toolHandler
}

// --- Entry point (wired into main's dispatch as `case "serve"`) ---

func cmdServe(args []string) {
	// --scope sets a default scope applied when a tool call omits one. Keeps
	// single-tenant agents from repeating the scope on every call.
	defaultScope := flagStr(args, "--scope", "default")

	srv := newMCPServer(os.Stdin, os.Stdout, defaultScope)
	if err := srv.serve(); err != nil && err != io.EOF {
		fmt.Fprintf(os.Stderr, "Error: serve: %v\n", err)
		os.Exit(1)
	}
}

func newMCPServer(in io.Reader, out io.Writer, defaultScope string) *mcpServer {
	s := &mcpServer{in: in, out: out, funcs: map[string]toolHandler{}}
	registerKBTools(s, defaultScope)
	return s
}

// register adds a tool and its handler to the server.
func (s *mcpServer) register(t mcpTool, h toolHandler) {
	s.tools = append(s.tools, t)
	s.funcs[t.Name] = h
}

// --- Transport loop: one JSON-RPC message per line over stdio ---

func (s *mcpServer) serve() error {
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

func (s *mcpServer) handleLine(line []byte) {
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

func (s *mcpServer) handleToolCall(req rpcRequest) {
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

func (s *mcpServer) writeResult(id json.RawMessage, result any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Result: result})
}

func (s *mcpServer) writeError(id json.RawMessage, code int, msg string, data any) {
	s.write(rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: msg, Data: data}})
}

func (s *mcpServer) write(resp rpcResponse) {
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

func registerKBTools(s *mcpServer, defaultScope string) {
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
		return mcpSearch(args, defaultScope)
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
		return mcpStats(args, defaultScope)
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
		return mcpList(args, defaultScope)
	})
}

// --- Tool handlers: reuse CLI primitives, return the CLI's --json shapes ---

// mcpSearch replicates cmdSearch's --json branch using the same primitives.
// Returns []map (BM25 path) or vector results, matching the CLI byte shape.
func mcpSearch(args map[string]any, defaultScope string) (any, error) {
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
		queryVec, err := loadVectorFromFile(queryVecPath)
		if err != nil {
			return nil, fmt.Errorf("load query vector: %v", err)
		}
		var results []vectorSearchResult
		if hybridMode {
			if query == "" {
				return nil, fmt.Errorf("hybrid requires a text query alongside query_vec_path")
			}
			results, err = runHybridSearch(scope, query, queryVec, topK)
		} else {
			results, err = runVectorSearch(scope, queryVec, topK)
		}
		if err != nil {
			return nil, err
		}
		return results, nil
	}

	scopes := resolveScopes(scope)

	var allArticles []*WikiArticle
	var scopeMap []string
	for _, sc := range scopes {
		articles, err := listArticles(sc)
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

	var results []*WikiArticle
	if len(scopes) == 1 {
		si := loadSearchIndex(scopes[0])
		results = bm25SearchWithIndex(allArticles, query, limit, si)
	} else {
		results = bm25Search(allArticles, query, limit)
	}

	resultScope := func(a *WikiArticle) string {
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

	a, err := loadArticle(scope, id)
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
// keyed lowercase in index.json (see rebuildIndex); the linked articles' titles
// and summaries are the canonical definition surface kb-go holds. Read-only.
func mcpGlossary(args map[string]any, defaultScope string) (any, error) {
	term := argStr(args, "term", "")
	if term == "" {
		return nil, fmt.Errorf("term is required")
	}
	scope := argStr(args, "scope", defaultScope)

	idx := loadIndex(scope)
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
		a, err := loadArticle(scope, aid)
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
func mcpStats(args map[string]any, defaultScope string) (any, error) {
	scope := argStr(args, "scope", defaultScope)

	articles, _ := listArticles(scope)
	idx := loadIndex(scope)
	rawCount := 0
	if entries, err := os.ReadDir(filepath.Join(scopeDir(scope), "raw")); err == nil {
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
		"vectors":    vectorIndexCount(scope),
	}, nil
}

// mcpList replicates cmdList's --json branch.
func mcpList(args map[string]any, defaultScope string) (any, error) {
	scope := argStr(args, "scope", defaultScope)

	articles, _ := listArticles(scope)
	out := make([]map[string]any, 0, len(articles))
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
	return out, nil
}
