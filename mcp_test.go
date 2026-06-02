// mcp_test.go — Tests for the read-only MCP stdio server (mcp.go).
//
// Covers:
//   - JSON-RPC handshake: initialize, tools/list (shape + tool set).
//   - tools/call for every tool (kb_search, kb_show, kb_glossary, kb_stats,
//     kb_list) against a sample KB built in a temp scope.
//   - Functional parity: the search result returned over the MCP transport is
//     byte-identical to `kb search --json` from the CLI for the same query.
//   - Latency delta: cold CLI spawn-per-query vs one persistent kb serve
//     process over N queries. Printed via t.Logf so the captain can see the
//     before/after number. Run with: go test -run MCP -v
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// --- sample KB fixture ---

// seedSampleKB writes a small set of articles + a rebuilt index + search index
// into a fresh real scope (under ~/.knowledge-base), the same way the CLI
// tests do. Returns the scope name and a cleanup func.
func seedSampleKB(t *testing.T) string {
	t.Helper()
	scope := "test-mcp-" + contentHash(t.Name())[:8]
	t.Cleanup(func() { os.RemoveAll(scopeDir(scope)) })

	articles := []*WikiArticle{
		{
			ID: "auth-middleware", Title: "Auth Middleware",
			Summary:    "Request authentication and token validation layer.",
			Content:    "# Auth Middleware\n\nValidates bearer tokens on every request.",
			Concepts:   []string{"authentication", "middleware", "tokens"},
			Categories: []string{"security"},
			WordCount:  42, CompiledWith: "test", Version: 1,
		},
		{
			ID: "rate-limiter", Title: "Rate Limiter",
			Summary:    "Sliding-window rate limiting for the API gateway.",
			Content:    "# Rate Limiter\n\nThrottles requests per client using a sliding window.",
			Concepts:   []string{"rate limiting", "middleware"},
			Categories: []string{"gateway"},
			WordCount:  37, CompiledWith: "test", Version: 1,
		},
		{
			ID: "search-index", Title: "Search Index",
			Summary:    "BM25 index construction and query path.",
			Content:    "# Search Index\n\nBuilds the inverted index used for BM25 ranking.",
			Concepts:   []string{"search", "bm25", "indexing"},
			Categories: []string{"retrieval"},
			WordCount:  55, CompiledWith: "test", Version: 1,
		},
	}
	for _, a := range articles {
		if err := saveArticle(scope, a); err != nil {
			t.Fatalf("saveArticle %s: %v", a.ID, err)
		}
	}
	if err := saveIndex(scope, rebuildIndex(scope, articles)); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}
	if err := saveSearchIndex(scope, buildSearchIndex(articles)); err != nil {
		t.Fatalf("saveSearchIndex: %v", err)
	}
	return scope
}

// roundtrip drives the in-process server with a slice of JSON-RPC request lines
// and returns the decoded responses (notifications produce no response).
func roundtrip(t *testing.T, scope string, requests ...string) []rpcResponse {
	t.Helper()
	in := strings.NewReader(strings.Join(requests, "\n") + "\n")
	var out bytes.Buffer
	srv := newMCPServer(in, &out, scope)
	if err := srv.serve(); err != nil && err != io.EOF {
		t.Fatalf("serve: %v", err)
	}
	var resps []rpcResponse
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r rpcResponse
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode response %q: %v", line, err)
		}
		resps = append(resps, r)
	}
	return resps
}

// callTool issues a single tools/call and returns the parsed text content of
// the result. Fails the test on transport error or tool isError.
func callTool(t *testing.T, scope, name string, args map[string]any) string {
	t.Helper()
	params, _ := json.Marshal(map[string]any{"name": name, "arguments": args})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":%s}`, params)
	resps := roundtrip(t, scope, req)
	if len(resps) != 1 {
		t.Fatalf("%s: expected 1 response, got %d", name, len(resps))
	}
	if resps[0].Error != nil {
		t.Fatalf("%s: rpc error: %+v", name, resps[0].Error)
	}
	return toolText(t, name, resps[0].Result)
}

// toolText extracts the single text-content block from a tools/call result and
// asserts isError is false.
func toolText(t *testing.T, name string, result any) string {
	t.Helper()
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("%s: result not an object: %T", name, result)
	}
	if isErr, _ := m["isError"].(bool); isErr {
		t.Fatalf("%s: tool reported error: %v", name, m["content"])
	}
	content, ok := m["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("%s: no content in result", name)
	}
	block := content[0].(map[string]any)
	return block["text"].(string)
}

// --- protocol tests ---

func TestMCPInitialize(t *testing.T) {
	scope := seedSampleKB(t)
	resps := roundtrip(t, scope, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	if len(resps) != 1 {
		t.Fatalf("expected 1 response, got %d", len(resps))
	}
	res := resps[0].Result.(map[string]any)
	if res["protocolVersion"] != mcpProtocolVersion {
		t.Errorf("protocolVersion = %v, want %v", res["protocolVersion"], mcpProtocolVersion)
	}
	info := res["serverInfo"].(map[string]any)
	if info["name"] != "kb-go" {
		t.Errorf("serverInfo.name = %v, want kb-go", info["name"])
	}
}

func TestMCPNotificationGetsNoResponse(t *testing.T) {
	scope := seedSampleKB(t)
	// initialized notification has no id — must produce zero responses.
	resps := roundtrip(t, scope, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if len(resps) != 0 {
		t.Fatalf("notification produced %d responses, want 0", len(resps))
	}
}

func TestMCPToolsList(t *testing.T) {
	scope := seedSampleKB(t)
	resps := roundtrip(t, scope, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	res := resps[0].Result.(map[string]any)
	tools := res["tools"].([]any)

	got := map[string]bool{}
	for _, tl := range tools {
		got[tl.(map[string]any)["name"].(string)] = true
	}
	want := []string{"kb_search", "kb_show", "kb_glossary", "kb_stats", "kb_list"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("tools/list missing %q", w)
		}
	}
	// Read-only guarantee: no mutation tools exposed.
	for _, banned := range []string{"kb_build", "kb_ingest", "kb_recompile", "kb_clear", "kb_accept"} {
		if got[banned] {
			t.Errorf("mutation tool %q must not be exposed over MCP", banned)
		}
	}
	if len(tools) != len(want) {
		t.Errorf("tools/list has %d tools, want %d", len(tools), len(want))
	}
}

func TestMCPUnknownMethod(t *testing.T) {
	scope := seedSampleKB(t)
	resps := roundtrip(t, scope, `{"jsonrpc":"2.0","id":9,"method":"does/not/exist"}`)
	if resps[0].Error == nil || resps[0].Error.Code != errMethodNotFound {
		t.Fatalf("expected method-not-found error, got %+v", resps[0])
	}
}

func TestMCPParseError(t *testing.T) {
	scope := seedSampleKB(t)
	resps := roundtrip(t, scope, `{not json`)
	if resps[0].Error == nil || resps[0].Error.Code != errParse {
		t.Fatalf("expected parse error, got %+v", resps[0])
	}
}

// --- per-tool tests ---

func TestMCPToolShow(t *testing.T) {
	scope := seedSampleKB(t)
	text := callTool(t, scope, "kb_show", map[string]any{"id": "auth-middleware", "scope": scope})
	var got map[string]any
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["title"] != "Auth Middleware" {
		t.Errorf("title = %v", got["title"])
	}
	if !strings.Contains(got["content"].(string), "bearer tokens") {
		t.Errorf("content missing body: %v", got["content"])
	}
}

func TestMCPToolShowNotFound(t *testing.T) {
	scope := seedSampleKB(t)
	params, _ := json.Marshal(map[string]any{"name": "kb_show", "arguments": map[string]any{"id": "nope", "scope": scope}})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":%s}`, params)
	resps := roundtrip(t, scope, req)
	m := resps[0].Result.(map[string]any)
	if isErr, _ := m["isError"].(bool); !isErr {
		t.Fatalf("expected isError true for missing article, got %+v", m)
	}
}

func TestMCPToolGlossary(t *testing.T) {
	scope := seedSampleKB(t)
	// "middleware" is a concept shared by two articles; lookup is case-insensitive.
	text := callTool(t, scope, "kb_glossary", map[string]any{"term": "MIDDLEWARE", "scope": scope})
	var got map[string]any
	json.Unmarshal([]byte(text), &got)
	if got["resolved"] != true {
		t.Fatalf("expected resolved=true, got %v", got)
	}
	if int(got["article_count"].(float64)) != 2 {
		t.Errorf("article_count = %v, want 2", got["article_count"])
	}

	// Unknown term resolves to false, not an error.
	text = callTool(t, scope, "kb_glossary", map[string]any{"term": "quantum", "scope": scope})
	json.Unmarshal([]byte(text), &got)
	if got["resolved"] != false {
		t.Errorf("expected resolved=false for unknown term, got %v", got)
	}
}

func TestMCPToolStats(t *testing.T) {
	scope := seedSampleKB(t)
	text := callTool(t, scope, "kb_stats", map[string]any{"scope": scope})
	var got map[string]any
	json.Unmarshal([]byte(text), &got)
	if int(got["articles"].(float64)) != 3 {
		t.Errorf("articles = %v, want 3", got["articles"])
	}
}

func TestMCPToolList(t *testing.T) {
	scope := seedSampleKB(t)
	text := callTool(t, scope, "kb_list", map[string]any{"scope": scope})
	var got []map[string]any
	json.Unmarshal([]byte(text), &got)
	if len(got) != 3 {
		t.Errorf("list returned %d articles, want 3", len(got))
	}
}

// --- functional parity: MCP transport vs CLI ---

// normalizeSearchJSON parses a search JSON payload and re-marshals it in a
// canonical key order so the two sources can be compared regardless of map
// iteration order.
func normalizeSearchJSON(t *testing.T, raw []byte) string {
	t.Helper()
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("decode search json %q: %v", raw, err)
	}
	canon, err := json.Marshal(arr)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(canon)
}

func TestMCPSearchParityWithCLI(t *testing.T) {
	scope := seedSampleKB(t)
	binary := buildTestBinary(t)
	const query = "middleware authentication"

	// CLI path: spawn the process, parse stdout JSON.
	cliOut, err := exec.Command(binary, "search", query, "--scope", scope, "--json", "--limit", "5").Output()
	if err != nil {
		t.Fatalf("cli search: %v", err)
	}
	cliCanon := normalizeSearchJSON(t, cliOut)

	// MCP path: drive the server over its real stdio transport (subprocess),
	// so this exercises the same code the agent host would.
	cmd := exec.Command(binary, "serve", "--scope", scope)
	stdin, _ := cmd.StdinPipe()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	callParams, _ := json.Marshal(map[string]any{
		"name":      "kb_search",
		"arguments": map[string]any{"query": query, "scope": scope, "limit": 5},
	})
	fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":%s}`+"\n", callParams)
	stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatalf("serve wait: %v\n%s", err, stdout.String())
	}

	// Pull the tools/call (id:2) response and extract its text content.
	var mcpCanon string
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var r rpcResponse
		if json.Unmarshal(sc.Bytes(), &r) != nil {
			continue
		}
		var id int
		json.Unmarshal(r.ID, &id)
		if id != 2 {
			continue
		}
		text := toolText(t, "kb_search", r.Result)
		mcpCanon = normalizeSearchJSON(t, []byte(text))
	}
	if mcpCanon == "" {
		t.Fatalf("no kb_search response found in serve output:\n%s", stdout.String())
	}

	if cliCanon != mcpCanon {
		t.Fatalf("parity mismatch:\n CLI: %s\n MCP: %s", cliCanon, mcpCanon)
	}
	t.Logf("parity OK: CLI and MCP return identical search JSON (%d bytes)", len(cliCanon))
}

// --- latency delta: cold CLI spawn vs persistent server ---

func TestMCPLatencyDelta(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency measurement in -short mode")
	}
	scope := seedSampleKB(t)
	binary := buildTestBinary(t)
	const n = 20
	queries := []string{
		"middleware authentication", "rate limiting", "bm25 search index",
		"tokens", "gateway throttle",
	}

	// (1) Cold CLI: spawn a fresh process for each query.
	cliStart := time.Now()
	for i := 0; i < n; i++ {
		q := queries[i%len(queries)]
		if _, err := exec.Command(binary, "search", q, "--scope", scope, "--json").Output(); err != nil {
			t.Fatalf("cli query %d: %v", i, err)
		}
	}
	cliTotal := time.Since(cliStart)

	// (2) Persistent server: one process, N tools/call over the same stdio.
	cmd := exec.Command(binary, "serve", "--scope", scope)
	stdin, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatalf("start serve: %v", err)
	}
	reader := bufio.NewReader(stdoutPipe)
	readLine := func() {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read serve response: %v", err)
		}
		_ = line
	}
	// Handshake (not counted).
	fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":0,"method":"initialize","params":{}}`)
	readLine()

	srvStart := time.Now()
	for i := 0; i < n; i++ {
		q := queries[i%len(queries)]
		callParams, _ := json.Marshal(map[string]any{
			"name":      "kb_search",
			"arguments": map[string]any{"query": q, "scope": scope},
		})
		fmt.Fprintf(stdin, `{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":%s}`+"\n", i+1, callParams)
		readLine()
	}
	srvTotal := time.Since(srvStart)
	stdin.Close()
	cmd.Wait()

	cliPer := cliTotal / n
	srvPer := srvTotal / n
	speedup := float64(cliPer) / float64(srvPer)

	t.Logf("latency over %d queries (sample KB, scope=%s):", n, scope)
	t.Logf("  cold CLI  (spawn/query): %v total, %v per query", cliTotal.Round(time.Microsecond), cliPer.Round(time.Microsecond))
	t.Logf("  persistent server      : %v total, %v per query", srvTotal.Round(time.Microsecond), srvPer.Round(time.Microsecond))
	t.Logf("  speedup                : %.1fx (per-query)", speedup)

	if srvPer >= cliPer {
		t.Errorf("expected persistent server to beat cold CLI per-query, got srv=%v cli=%v", srvPer, cliPer)
	}
}
