// Article compilation through a caller-owned compiler. kb holds no LLM client:
// it builds the compile prompt (buildCompilePrompt, shared with `kb prepare`),
// hands it to the user's `--compiler` command (or KB_COMPILER) on stdin, and
// parses ONE JSON article object from that command's stdout. The caller picks
// the model and pays for it through their own metered path (an agent backend,
// a LiteLLM/OpenAI-compatible gateway, a local Claude Code login).
//
// Invariants:
//   - A failed compile (non-zero exit, timeout, unparseable or incomplete
//     output) is an error for that item. Callers never store the raw text as
//     the article in its place; `kb ingest --allow-fallback` is the only
//     explicit verbatim path.
//   - The command runs through the platform shell (compiler_shell_*.go) so the
//     same string works as typed in a terminal; its stderr is passed through
//     line by line, prefixed with the source being compiled.
//   - `usage` in the output is optional and parsed leniently: wrong-typed or
//     unknown keys are dropped, never fatal.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const defaultCompilerTimeout = 300 * time.Second

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

// compilerSpec is the resolved `--compiler` hook. A zero Command means no
// compiler is configured.
type compilerSpec struct {
	Command string
	Timeout time.Duration
	Stderr  io.Writer // where the compiler's stderr is relayed; nil = os.Stderr
}

func (c compilerSpec) enabled() bool { return strings.TrimSpace(c.Command) != "" }

// label is the default compiled_with value: "compiler:<first word>", with
// quotes and directories stripped from that word.
func (c compilerSpec) label() string {
	cmd := strings.TrimSpace(c.Command)
	var first string
	if strings.HasPrefix(cmd, `"`) || strings.HasPrefix(cmd, `'`) {
		q := cmd[:1]
		if end := strings.Index(cmd[1:], q); end >= 0 {
			first = cmd[1 : end+1]
		} else {
			first = cmd[1:]
		}
	} else if f := strings.Fields(cmd); len(f) > 0 {
		first = f[0]
	}
	first = filepath.Base(strings.ReplaceAll(first, `\`, "/"))
	return "compiler:" + first
}

// compilerFromArgs resolves the hook: --compiler wins over KB_COMPILER.
// --compiler-timeout takes a Go duration ("90s", "2m") or whole seconds.
func compilerFromArgs(args []string) (compilerSpec, error) {
	spec := compilerSpec{
		Command: flagStr(args, "--compiler", os.Getenv("KB_COMPILER")),
		Timeout: defaultCompilerTimeout,
	}
	if raw := flagStr(args, "--compiler-timeout", ""); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			n, nerr := strconv.Atoi(raw)
			if nerr != nil {
				return spec, fmt.Errorf("--compiler-timeout %q: want seconds or a duration like 90s", raw)
			}
			d = time.Duration(n) * time.Second
		}
		if d <= 0 {
			return spec, fmt.Errorf("--compiler-timeout must be positive")
		}
		spec.Timeout = d
	}
	return spec, nil
}

// mustCompilerFromArgs is compilerFromArgs for commands: a bad flag is a usage
// error (exit 2), and the removed --model flag is rejected with a pointer to
// --compiler instead of being silently ignored.
func mustCompilerFromArgs(args []string) compilerSpec {
	if flagBool(args, "--model") {
		usageExit("--model was removed in kb v0.4.0: kb no longer calls an LLM itself.\n" +
			"  Pick the model inside your --compiler command (e.g. `claude -p --model haiku ...`,\n" +
			"  or KB_COMPILE_MODEL for examples/compilers/openai_compatible.py).")
	}
	spec, err := compilerFromArgs(args)
	if err != nil {
		usageExit(err.Error())
	}
	return spec
}

// requireCompiler exits 2 with guidance when no compiler is configured.
func requireCompiler(spec compilerSpec, command, alternative string) {
	if spec.enabled() {
		return
	}
	msg := fmt.Sprintf("kb %s needs a compiler: kb does not call an LLM itself.\n"+
		"  Pass --compiler \"<command>\" (or set KB_COMPILER): kb writes each prompt to the\n"+
		"  command's stdin and reads one JSON article from its stdout.\n", command)
	if alternative != "" {
		msg += "  " + alternative + "\n"
	}
	msg += "  Recipes: README.md, \"Compiling articles\"."
	usageExit(msg)
}

func usageExit(msg string) {
	fmt.Fprintln(os.Stderr, "Error: "+msg)
	os.Exit(2)
}

// codeContextBlock renders the AST context section of the compile prompt.
func codeContextBlock(codeMod *CodeModule) string {
	if codeMod == nil {
		return ""
	}
	return fmt.Sprintf("\nAST-extracted structure:\n```\n%s```\n\n", formatCodeContext(codeMod))
}

// buildCompilePrompt constructs the compilation prompt shared by the compiler
// hook and cmdPrepare. When terse is true, the prompt targets 120-180 words
// for agent context budgets; otherwise 400-800 words for human documentation.
func buildCompilePrompt(source, contextBlock, rawText string, terse bool) string {
	var instructions string
	if terse {
		instructions = `Target 120-180 words. Write an overview, not a deep explanation.
Cover: what the component does (1 sentence), its role in the system (1-2 sentences),
key entry points (bullet list, max 5).`
	} else {
		instructions = `Target 400-800 words of thorough explanation.
- Explain WHY code exists, not just what it does. For defensive patterns, edge cases, and workarounds, explain what failure they prevent.
- Flag any TODO, FIXME, HACK, or incomplete implementations as "Known Gaps" in the content.
- If you see idempotency guards, retry logic, or error handling, explain the failure scenario that motivated them.`
	}
	return fmt.Sprintf(`Compile this source into a structured knowledge article.
Source: %s
%s
%s

Output ONLY valid JSON with these exact keys:
{"title":"descriptive title","summary":"2-3 sentence overview","content":"full markdown article with a Known Gaps section if applicable","concepts":["key","entities"],"categories":["broad","topics"]}

Source text:
%s`, source, contextBlock, instructions, rawText)
}

// compileWithHook compiles one source through the compiler hook. On any
// failure it returns (nil, err): the caller decides how to report, and must
// not substitute the raw text.
func compileWithHook(spec compilerSpec, rawText, source string, codeMod *CodeModule, terse bool) (*WikiArticle, error) {
	prompt := buildCompilePrompt(source, codeContextBlock(codeMod), rawText, terse)
	out, err := runCompiler(spec, prompt, source)
	if err != nil {
		return nil, err
	}
	res, err := parseCompiledArticle(out)
	if err != nil {
		return nil, err
	}

	audience, depth, targetWords := "human", "deep", 500
	if terse {
		audience, depth, targetWords = "agent", "overview", 150
	}
	return &WikiArticle{
		ID:           slugify(res.Title),
		Title:        res.Title,
		Summary:      res.Summary,
		Content:      res.Content,
		Concepts:     nilToEmpty(res.Concepts),
		Categories:   nilToEmpty(res.Categories),
		WordCount:    wordCount(res.Content),
		CompiledAt:   time.Now().UTC().Format(time.RFC3339),
		CompiledWith: compiledWithFor(res.CompiledWith, res.Usage, spec.label()),
		Version:      1,
		Audience:     audience,
		Depth:        depth,
		TargetWords:  targetWords,
		Usage:        res.Usage,
	}, nil
}

// compiledWithFor picks compiled_with: explicit value, else usage.model, else
// the caller's default.
func compiledWithFor(explicit string, usage *ArticleUsage, fallback string) string {
	if explicit != "" {
		return explicit
	}
	if usage != nil && usage.Model != "" {
		return usage.Model
	}
	return fallback
}

// compiledArticle is the article object a compiler prints.
type compiledArticle struct {
	Title        string
	Summary      string
	Content      string
	Concepts     []string
	Categories   []string
	CompiledWith string
	Usage        *ArticleUsage
}

// parseCompiledArticle extracts one JSON article object from compiler output.
// Tolerates ```json fences and a stray line around the object; requires title
// and content.
func parseCompiledArticle(out []byte) (*compiledArticle, error) {
	text := stripFences(string(out))
	var raw struct {
		Title        string          `json:"title"`
		Summary      string          `json:"summary"`
		Content      string          `json:"content"`
		Concepts     []string        `json:"concepts"`
		Categories   []string        `json:"categories"`
		CompiledWith string          `json:"compiled_with"`
		Usage        json.RawMessage `json:"usage"`
	}
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		// Fall back to the outermost {...} span (models sometimes add a line).
		i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
		if i < 0 || j <= i || json.Unmarshal([]byte(text[i:j+1]), &raw) != nil {
			return nil, fmt.Errorf("compiler output is not a JSON article object: %v; output starts: %q", err, text[:min(len(text), 200)])
		}
	}
	if strings.TrimSpace(raw.Title) == "" || strings.TrimSpace(raw.Content) == "" {
		return nil, fmt.Errorf("compiler output is missing title or content")
	}
	return &compiledArticle{
		Title: raw.Title, Summary: raw.Summary, Content: raw.Content,
		Concepts: raw.Concepts, Categories: raw.Categories,
		CompiledWith: raw.CompiledWith, Usage: parseUsage(raw.Usage),
	}, nil
}

func stripFences(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// parseUsage reads the optional usage object leniently: each known key is
// taken only when it has the right type; anything else is ignored. Returns
// nil when nothing usable is present.
func parseUsage(raw json.RawMessage) *ArticleUsage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil
	}
	u := &ArticleUsage{}
	if s, ok := m["model"].(string); ok {
		u.Model = s
	}
	if f, ok := m["input_tokens"].(float64); ok && f >= 0 {
		u.InputTokens = int(f)
	}
	if f, ok := m["output_tokens"].(float64); ok && f >= 0 {
		u.OutputTokens = int(f)
	}
	if f, ok := m["cost_usd"].(float64); ok && f >= 0 {
		u.CostUSD = f
	}
	if *u == (ArticleUsage{}) {
		return nil
	}
	return u
}

// usageTotals sums usage across articles: how many carry usage, and their
// input/output tokens and cost.
func usageTotals(articles []*WikiArticle) (n, in, out int, cost float64) {
	for _, a := range articles {
		if a == nil || a.Usage == nil {
			continue
		}
		n++
		in += a.Usage.InputTokens
		out += a.Usage.OutputTokens
		cost += a.Usage.CostUSD
	}
	return
}

// runCompiler runs the hook once: prompt on stdin, stdout returned. Non-zero
// exit and timeout are errors. label names the item in relayed stderr lines.
func runCompiler(spec compilerSpec, prompt, label string) ([]byte, error) {
	if !spec.enabled() {
		return nil, errors.New("no compiler configured")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = defaultCompilerTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	errDst := spec.Stderr
	if errDst == nil {
		errDst = os.Stderr
	}
	pw := &prefixWriter{dst: errDst, prefix: "[compiler " + label + "] "}

	cmd := shellCommand(ctx, spec.Command)
	cmd.Stdin = strings.NewReader(prompt)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = pw
	// If the shell is killed but a grandchild still holds the pipes, stop
	// waiting for them shortly after.
	cmd.WaitDelay = 5 * time.Second

	err := cmd.Run()
	pw.flush()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("compiler timed out after %s", timeout)
	}
	if err != nil {
		return nil, fmt.Errorf("compiler failed (%v)", err)
	}
	return stdout.Bytes(), nil
}

// prefixWriter relays a child's stderr line by line with a prefix. Parallel
// compiles share the destination, so each line is written under a
// package-wide lock to keep lines from interleaving.
type prefixWriter struct {
	dst    io.Writer
	prefix string
	buf    []byte
}

var stderrRelayMu sync.Mutex

func (p *prefixWriter) Write(b []byte) (int, error) {
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexByte(p.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(p.buf[:i]), "\r")
		p.buf = p.buf[i+1:]
		stderrRelayMu.Lock()
		fmt.Fprintln(p.dst, p.prefix+line)
		stderrRelayMu.Unlock()
	}
	return len(b), nil
}

func (p *prefixWriter) flush() {
	if len(p.buf) == 0 {
		return
	}
	stderrRelayMu.Lock()
	fmt.Fprintln(p.dst, p.prefix+strings.TrimRight(string(p.buf), "\r"))
	stderrRelayMu.Unlock()
	p.buf = nil
}
