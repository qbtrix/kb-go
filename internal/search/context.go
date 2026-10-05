// `kb search --context` text assembly: whole articles when they fit the
// budget, query-focused section excerpts when they don't.

package search

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/qbtrix/kb-go/internal/model"
)

// Context budgets are byte counts (len of the UTF-8 string), which is what
// callers measure when they cut blocks. 4000 per article returns a typical
// compiled article (roughly 1.5k-3.5k chars) whole while still leaving room for
// a second article inside the 8000 total.
const (
	DefaultContextChars = 4000
	DefaultContextTotal = 8000
	// A lower-ranked article gets an excerpt only if at least this much of the
	// total budget is left; below it, only articles that fit whole are added.
	minContextExcerpt = 400
	// contextSeparator joins article blocks. PocketPaw splits on exactly this
	// string and maps each "## Title" to its body, so it must not change. Note
	// that article content can itself contain a "---" rule surrounded by blank
	// lines; consumers splitting on the separator would mis-split those.
	contextSeparator = "\n\n---\n\n"
	contextSkip      = "\n\n…\n\n" // marks content left out of an excerpt
)

// contextBlock is one article's context: Block is the "## Title\n..." text
// the plain --context output prints; Truncated is true when it is an excerpt.
type contextBlock struct {
	Article   *model.WikiArticle
	Block     string
	Truncated bool
}

// FormatContext renders ranked results as prompt-ready text: the
// searchContextBlocks joined by contextSeparator.
func FormatContext(results []*model.WikiArticle, query string, perArticle, totalCap int) string {
	var parts []string
	for _, b := range searchContextBlocks(results, query, perArticle, totalCap) {
		parts = append(parts, b.Block)
	}
	return strings.Join(parts, contextSeparator)
}

// ContextJSON is the --context --json shape: one entry per article with
// the excerpt body (no "## Title" line). No in-band separator, so bodies that
// contain "---" rules survive intact.
func ContextJSON(results []*model.WikiArticle, query string, perArticle, totalCap int) []map[string]any {
	out := []map[string]any{}
	for _, b := range searchContextBlocks(results, query, perArticle, totalCap) {
		text := ""
		if _, rest, ok := strings.Cut(b.Block, "\n"); ok {
			text = rest
		}
		out = append(out, map[string]any{"id": b.Article.ID, "title": b.Article.Title, "text": text, "truncated": b.Truncated})
	}
	return out
}

// searchContextBlocks builds one contextExcerpt per ranked article within the
// per-article and total budgets (total counts the text separators, so both
// output modes return the same excerpts). The total cap never drops the best
// hit; it is trimmed to fit instead.
func searchContextBlocks(results []*model.WikiArticle, query string, perArticle, totalCap int) []contextBlock {
	var parts []contextBlock
	total := 0
	for i, a := range results {
		budget := min(perArticle, totalCap)
		if i > 0 {
			budget = min(perArticle, totalCap-total-len(contextSeparator))
			if budget <= 0 {
				break
			}
			if budget < minContextExcerpt && len(contextFullBlock(a)) > budget {
				continue
			}
		}
		block := contextExcerpt(a, query, budget)
		if block == "" {
			continue
		}
		if len(parts) > 0 {
			total += len(contextSeparator)
		}
		parts = append(parts, contextBlock{Article: a, Block: block, Truncated: block != contextFullBlock(a)})
		total += len(block)
	}
	return parts
}

func contextFullBlock(a *model.WikiArticle) string {
	return "## " + contextOneLine(a.Title) + "\n" + strings.TrimSpace(strings.ReplaceAll(a.Content, "\r\n", "\n"))
}

// contextExcerpt returns "## <Title>\n" plus as much of the article as fits in
// budget bytes. If the whole article fits it is returned verbatim. Otherwise it
// builds a query-focused excerpt: the summary line for orientation, then the
// article's sections (split at markdown headings; oversized sections split
// further into blank-line blocks, with tables, lists and code fences atomic)
// ranked by BM25 over the article's own sections using the search tokenizer,
// heading terms weighted 3x. Chosen pieces are emitted in document order with
// their heading, and "…" marks skipped content. No matching section means the
// article's head is used instead. A table is never cut mid-row; a table that
// alone exceeds the budget keeps its header and as many whole rows as fit.
func contextExcerpt(a *model.WikiArticle, query string, budget int) string {
	full := contextFullBlock(a)
	if len(full) <= budget {
		return full
	}
	title := "## " + contextOneLine(a.Title)
	if len(title)+1 >= budget {
		return contextCutBytes(title, budget)
	}
	head := title + "\n"
	if s := contextOneLine(a.Summary); s != "" && len(s)+1 <= budget/4 {
		head += s + "\n"
	}
	head += "\n"
	avail := budget - len(head)

	secs := parseContextSections(strings.ReplaceAll(a.Content, "\r\n", "\n"))
	if len(secs) == 0 || avail <= 0 {
		return strings.TrimRight(head, "\n")
	}

	// Units: a whole section when it could fit at all, else its blocks.
	// start/end are positions in the flattened block order, used to decide
	// whether two emitted pieces are contiguous.
	type unit struct {
		sec, blk   int // blk < 0: whole section
		text       string
		score      float64
		start, end int
	}
	scoreOf := contextScorer(secs, query)
	var units []unit
	pos := 0
	for i, s := range secs {
		n := max(1, len(s.blocks))
		if len(s.raw) <= avail || len(s.blocks) == 0 {
			units = append(units, unit{sec: i, blk: -1, text: s.raw, score: scoreOf(i, ""), start: pos, end: pos + n - 1})
		} else {
			for j, b := range s.blocks {
				units = append(units, unit{sec: i, blk: j, text: b, score: scoreOf(i, b), start: pos + j, end: pos + j})
			}
		}
		pos += n
	}

	matched := false
	for _, u := range units {
		if u.score > 0 {
			matched = true
			break
		}
	}
	order := make([]int, len(units))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(x, y int) bool { return units[order[x]].score > units[order[y]].score })

	// add emits unit idx if it fits; with trim, an over-budget unit is cut to
	// the room left (whole lines, whole table rows) instead of skipped.
	var chosen []unit
	taken := map[int]bool{}
	headed := map[int]bool{}
	used := 0
	add := func(idx int, trim bool) bool {
		u := units[idx]
		// Upper-bound cost: separator + (heading for a block's first piece) + text.
		overhead := 0
		if len(chosen) > 0 {
			overhead += len(contextSkip)
		}
		if u.blk >= 0 && !headed[u.sec] && secs[u.sec].heading != "" {
			overhead += len(secs[u.sec].heading) + len(contextSkip)
		}
		room := avail - used - overhead
		if len(u.text) > room {
			if !trim {
				return false
			}
			if u.text = contextTrimBlock(u.text, room); u.text == "" {
				return false
			}
		}
		chosen = append(chosen, u)
		taken[idx] = true
		used += overhead + len(u.text)
		if u.blk >= 0 {
			headed[u.sec] = true
		}
		return true
	}
	if matched {
		// Best units first; only the top one may be trimmed rather than skipped.
		for _, idx := range order {
			if units[idx].score > 0 {
				add(idx, len(chosen) == 0)
			}
		}
		// Then the rest of any section a chosen block came from, in document
		// order, so a matching intro brings its table along (rows trimmed).
		for idx, u := range units {
			if !taken[idx] && u.blk >= 0 && headed[u.sec] {
				add(idx, true)
			}
		}
	} else {
		// Nothing matched: the article's head, contiguous, the last unit trimmed.
		for idx := range units {
			if !add(idx, true) || chosen[len(chosen)-1].text != units[idx].text {
				break
			}
		}
	}
	if len(chosen) == 0 {
		return strings.TrimRight(head, "\n")
	}

	sort.SliceStable(chosen, func(x, y int) bool { return chosen[x].start < chosen[y].start })
	var body strings.Builder
	prevEnd, prevSec := -2, -1
	for _, u := range chosen {
		if body.Len() > 0 {
			if u.start == prevEnd+1 {
				body.WriteString("\n\n")
			} else {
				body.WriteString(contextSkip)
			}
		}
		if u.blk >= 0 && u.sec != prevSec && secs[u.sec].heading != "" {
			body.WriteString(secs[u.sec].heading)
			if u.blk == 0 {
				body.WriteString("\n\n")
			} else {
				body.WriteString(contextSkip)
			}
		}
		body.WriteString(u.text)
		prevEnd, prevSec = u.end, u.sec
	}
	out := head + body.String()
	if len(out) > budget { // defensive: the cost bounds above should prevent this
		out = contextTrimBlock(out, budget)
	}
	return out
}

// contextSection is one markdown section of an article: its heading line
// ("" for text before the first heading), its full text, and its body split
// into atomic blocks.
type contextSection struct {
	heading string
	raw     string
	blocks  []string
}

func isMarkdownHeading(line string) bool {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && n < len(line) && (line[n] == ' ' || line[n] == '\t')
}

func isFenceLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

func isListLine(line string) bool {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "+ ") {
		return true
	}
	i := 0
	for i < len(t) && t[i] >= '0' && t[i] <= '9' {
		i++
	}
	return i > 0 && i+1 < len(t) && (t[i] == '.' || t[i] == ')') && t[i+1] == ' '
}

func isTableLine(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "|")
}

// parseContextSections splits content at markdown headings (ignoring '#'
// lines inside code fences). Blocks are runs of non-blank lines, so a table
// or a tight list stays whole; blank-separated list items are merged back
// into one list block.
func parseContextSections(content string) []contextSection {
	var secs []contextSection
	var heading string
	var body []string
	flush := func() {
		raw := strings.TrimSpace(strings.Join(body, "\n"))
		if heading != "" {
			raw = strings.TrimSpace(heading + "\n\n" + raw)
		}
		if raw != "" {
			secs = append(secs, contextSection{heading: heading, raw: raw, blocks: splitContextBlocks(body)})
		}
	}
	inFence := false
	for _, line := range strings.Split(content, "\n") {
		if !inFence && isMarkdownHeading(line) {
			flush()
			heading, body = strings.TrimSpace(line), nil
			continue
		}
		if isFenceLine(line) {
			inFence = !inFence
		}
		body = append(body, line)
	}
	flush()
	return secs
}

func splitContextBlocks(lines []string) []string {
	var blocks []string
	var cur []string
	inFence := false
	end := func() {
		if len(cur) > 0 {
			b := strings.Join(cur, "\n")
			if n := len(blocks); n > 0 && isListLine(cur[0]) && isListLine(strings.SplitN(blocks[n-1], "\n", 2)[0]) {
				blocks[n-1] += "\n\n" + b
			} else {
				blocks = append(blocks, b)
			}
			cur = nil
		}
	}
	for _, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(line) == "" {
			end()
			continue
		}
		cur = append(cur, strings.TrimRight(line, " \t"))
	}
	end()
	return blocks
}

// contextScorer returns a BM25 scorer over the article's sections. score(i, "")
// scores section i; score(i, block) scores one block of it. Heading tokens are
// counted 3x in both, so a query word that only appears in a heading
// ("Footwear") still lifts that section's blocks.
func contextScorer(secs []contextSection, query string) func(sec int, block string) float64 {
	seen := map[string]bool{}
	var terms []string
	for _, t := range Tokenize(query) {
		if !seen[t] {
			seen[t] = true
			terms = append(terms, t)
		}
	}
	docTokens := func(heading, body string) []string {
		h := Tokenize(heading)
		toks := append(append(append([]string{}, h...), h...), h...)
		return append(toks, Tokenize(body)...)
	}
	secToks := make([][]string, len(secs))
	df := map[string]int{}
	totalLen := 0
	for i, s := range secs {
		secToks[i] = docTokens(s.heading, strings.Join(s.blocks, "\n"))
		totalLen += len(secToks[i])
		present := map[string]bool{}
		for _, t := range secToks[i] {
			present[t] = true
		}
		for _, t := range terms {
			if present[t] {
				df[t]++
			}
		}
	}
	n := float64(len(secs))
	avgdl := math.Max(1, float64(totalLen)/n)
	const k1, b = 1.2, 0.75
	return func(sec int, block string) float64 {
		toks := secToks[sec]
		if block != "" {
			toks = docTokens(secs[sec].heading, block)
		}
		tf := map[string]int{}
		for _, t := range toks {
			if seen[t] {
				tf[t]++
			}
		}
		dl := float64(len(toks))
		score := 0.0
		for _, t := range terms {
			f := float64(tf[t])
			if f == 0 {
				continue
			}
			d := float64(df[t])
			idf := math.Log(1 + (n-d+0.5)/(d+0.5))
			score += idf * f * (k1 + 1) / (f + k1*(1-b+b*dl/avgdl))
		}
		return score
	}
}

// contextTrimBlock cuts a block to at most max bytes at line boundaries and
// appends "…" when anything was dropped. Table rows are never split, and a
// table is kept only with its header, separator and at least one row. A lone
// non-table line longer than max is cut at a word boundary.
func contextTrimBlock(block string, max int) string {
	if len(block) <= max {
		return block
	}
	const marker = "\n…"
	lines := strings.Split(block, "\n")
	var kept []string
	size := 0
	for _, l := range lines {
		add := len(l)
		if len(kept) > 0 {
			add++
		}
		if size+add > max-len(marker) {
			break
		}
		kept = append(kept, l)
		size += add
	}
	// Drop a trailing table fragment that has no data row yet.
	tail := 0
	for tail < len(kept) && isTableLine(kept[len(kept)-1-tail]) {
		tail++
	}
	if tail > 0 && tail < 3 {
		kept = kept[:len(kept)-tail]
	}
	if len(kept) == 0 {
		if isTableLine(lines[0]) || max <= len("…") {
			return ""
		}
		return contextCutBytes(lines[0], max-len("…")) + "…"
	}
	return strings.Join(kept, "\n") + marker
}

// contextCutBytes cuts s to at most max bytes on a rune boundary, preferring
// the last space in the second half of the cut.
func contextCutBytes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	s = s[:cut]
	if i := strings.LastIndexByte(s, ' '); i > cut/2 {
		s = s[:i]
	}
	return strings.TrimRight(s, " ")
}

func contextOneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
