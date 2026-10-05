// Package compile turns source text into a wiki article. Article picks the
// compile path for a resolved Spec: the --compiler hook when Spec.Command is
// set (Run pipes the prompt to the caller's command on stdin and parses ONE
// JSON article object from its stdout), else the built-in Anthropic client
// (anthropic.go) when Spec.APIKey is set. Prompt builds the compile prompt,
// shared with `kb prepare`. Resolving the Spec from flags and env, with the
// precedence --compiler > KB_COMPILER > built-in client > none, is the CLI's
// job.
//
// Invariants:
//   - A failed compile (hook: non-zero exit, timeout, unparseable or
//     incomplete output; built-in: transport error, non-200, unparseable
//     reply) is an error for that item. Callers never store the raw text as
//     the article in its place; `kb ingest --allow-fallback` is the only
//     explicit verbatim path.
//   - The hook runs through the platform shell (shell_unix.go /
//     shell_windows.go) so the same string works as typed in a terminal; its
//     stderr is passed through line by line, prefixed with the source being
//     compiled.
//   - `usage` in hook output is optional and parsed leniently (ParseUsage):
//     wrong-typed or unknown keys are dropped, never fatal.
//   - compile does not import parse: callers render the prompt's code
//     section with parse.PromptBlock and pass it in as a string.
package compile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/qbtrix/kb-go/internal/model"
	"github.com/qbtrix/kb-go/internal/textutil"
)

const DefaultTimeout = 300 * time.Second

// Spec is the resolved compile path. A non-empty Command selects the
// hook; otherwise a non-empty APIKey selects the built-in Anthropic client;
// neither means no compiler is configured.
type Spec struct {
	Command string
	Timeout time.Duration
	Stderr  io.Writer // where the compiler's stderr is relayed; nil = os.Stderr

	// Built-in client (anthropic.go).
	APIKey  string
	Model   string
	BaseURL string
}

func (c Spec) Hook() bool    { return strings.TrimSpace(c.Command) != "" }
func (c Spec) Builtin() bool { return !c.Hook() && strings.TrimSpace(c.APIKey) != "" }
func (c Spec) Enabled() bool { return c.Hook() || c.Builtin() }

// Label is the default compiled_with value: "compiler:<first word>", with
// quotes and directories stripped from that word.
func (c Spec) Label() string {
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

// Prompt constructs the compilation prompt shared by the built-in
// client, the compiler hook and cmdPrepare. When terse is true, the prompt
// targets 120-180 words for agent context budgets; otherwise 400-800 words
// for human documentation.
func Prompt(source, contextBlock, rawText string, terse bool) string {
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

// Article compiles one source through the configured path (hook, else
// built-in client). contextBlock is the prompt's optional code-structure
// section (parse.PromptBlock, or ""). On any failure it returns (nil, err):
// the caller decides how to report, and must not substitute the raw text.
func Article(spec Spec, rawText, source, contextBlock string, terse bool) (*model.WikiArticle, error) {
	switch {
	case spec.Hook():
		return hookArticle(spec, rawText, source, contextBlock, terse)
	case spec.Builtin():
		return builtinArticle(spec, rawText, source, contextBlock, terse)
	}
	return nil, errors.New("no compiler configured")
}

// hookArticle compiles one source through the compiler hook.
func hookArticle(spec Spec, rawText, source, contextBlock string, terse bool) (*model.WikiArticle, error) {
	prompt := Prompt(source, contextBlock, rawText, terse)
	out, err := Run(spec, prompt, source)
	if err != nil {
		return nil, err
	}
	res, err := parseCompiledArticle(out)
	if err != nil {
		return nil, err
	}
	return newArticle(res, terse, CompiledWith(res.CompiledWith, res.Usage, spec.Label()), res.Usage), nil
}

// newArticle builds the stored article from a parsed compile result.
func newArticle(res *compiledArticle, terse bool, compiledWith string, usage *model.ArticleUsage) *model.WikiArticle {
	audience, depth, targetWords := "human", "deep", 500
	if terse {
		audience, depth, targetWords = "agent", "overview", 150
	}
	return &model.WikiArticle{
		ID:           textutil.Slugify(res.Title),
		Title:        res.Title,
		Summary:      res.Summary,
		Content:      res.Content,
		Concepts:     textutil.NilToEmpty(res.Concepts),
		Categories:   textutil.NilToEmpty(res.Categories),
		WordCount:    textutil.WordCount(res.Content),
		CompiledAt:   time.Now().UTC().Format(time.RFC3339),
		CompiledWith: compiledWith,
		Version:      1,
		Audience:     audience,
		Depth:        depth,
		TargetWords:  targetWords,
		Usage:        usage,
	}
}

// CompiledWith picks compiled_with: explicit value, else usage.model, else
// the caller's default.
func CompiledWith(explicit string, usage *model.ArticleUsage, fallback string) string {
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
	Usage        *model.ArticleUsage
}

// parseCompiledArticle extracts one JSON article object from compiler output.
// Tolerates ```json fences and a stray line around the object; requires title
// and content.
func parseCompiledArticle(out []byte) (*compiledArticle, error) {
	text := StripFences(string(out))
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
		CompiledWith: raw.CompiledWith, Usage: ParseUsage(raw.Usage),
	}, nil
}

func StripFences(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	return strings.TrimSpace(text)
}

// ParseUsage reads the optional usage object leniently: each known key is
// taken only when it has the right type; anything else is ignored. Returns
// nil when nothing usable is present.
func ParseUsage(raw json.RawMessage) *model.ArticleUsage {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil || m == nil {
		return nil
	}
	u := &model.ArticleUsage{}
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
	if *u == (model.ArticleUsage{}) {
		return nil
	}
	return u
}

// UsageTotals sums usage across articles: how many carry usage, and their
// input/output tokens and cost.
func UsageTotals(articles []*model.WikiArticle) (n, in, out int, cost float64) {
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

// Run runs the hook once: prompt on stdin, stdout returned. Non-zero
// exit and timeout are errors. label names the item in relayed stderr lines.
func Run(spec Spec, prompt, label string) ([]byte, error) {
	if !spec.Hook() {
		return nil, errors.New("no compiler configured")
	}
	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
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
