// Streaming tokenizer for index builds: lowercases rune by rune while it scans,
// splits on non-letter/non-digit runes and stems each token (porter.go) through a
// per-build memo, without building a lowered copy of the text or a token
// slice.
//
// Invariant: the token stream is byte-identical to Tokenize's for any input
// (TestStreamTokenizerMatchesTokenize). Tokenize lowers the whole string with
// strings.ToLower, which is unicode.ToLower per rune (invalid UTF-8 becomes
// U+FFFD, a separator), then splits on the LOWERED runes; this scanner lowers
// each rune and classifies the lowered rune, so both cut at the same places
// and emit the same bytes. Tokenizing fields one at a time equals tokenizing
// them joined with " ", because a token never spans a space.

package search

import (
	"unicode"
	"unicode/utf8"
)

// tokenStream is one build's tokenizer state: a reusable token buffer and a
// stem memo (raw lowered token -> stem). Not safe for concurrent use.
type tokenStream struct {
	buf   []byte
	stems map[string]string
}

func newTokenStream() *tokenStream {
	return &tokenStream{buf: make([]byte, 0, 64), stems: make(map[string]string, 4096)}
}

// each calls fn with every stemmed token of text, in order.
func (ts *tokenStream) each(text string, fn func(tok string)) {
	buf := ts.buf[:0]
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			if ('a' <= c && c <= 'z') || ('0' <= c && c <= '9') {
				buf = append(buf, c)
			} else if len(buf) > 0 {
				fn(ts.stem(buf))
				buf = buf[:0]
			}
			i++
			continue
		}
		r, w := utf8.DecodeRuneInString(text[i:])
		i += w
		lr := unicode.ToLower(r)
		if unicode.IsLetter(lr) || unicode.IsDigit(lr) {
			buf = utf8.AppendRune(buf, lr)
		} else if len(buf) > 0 {
			fn(ts.stem(buf))
			buf = buf[:0]
		}
	}
	if len(buf) > 0 {
		fn(ts.stem(buf))
		buf = buf[:0]
	}
	ts.buf = buf
}

// stem returns stem(string(raw)), memoized for the build.
func (ts *tokenStream) stem(raw []byte) string {
	if s, ok := ts.stems[string(raw)]; ok {
		return s
	}
	key := string(raw)
	s := stem(key)
	ts.stems[key] = s
	return s
}
