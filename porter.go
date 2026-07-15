// porter.go — Vendored, dependency-free Porter (1980) English stemmer.
// Created: 2026-07-15 (feat/bm25-stemming).
//
// Why: kb-go's BM25 search is lexical and non-stemming, so "opens" in a doc
// was never retrieved by the query "open" (a real recall gap found in a live
// concierge smoke). This is a faithful Go port of Martin Porter's reference
// implementation (https://tartarus.org/martin/PorterStemmer/), applied inside
// tokenize() so index-time and query-time tokens are stemmed by the SAME
// function — "opens"->"open" then matches query "open"->"open".
//
// Conservative choices (both are in Porter's own reference, marked DEPARTURE):
//   - Words of length <= 2 are returned unchanged (never mangle "is", "as").
//   - step2 uses "bli"->"ble" and adds "logi"->"log".
// Additionally, tokens containing anything other than a-z (digits, symbols
// left by the tokenizer such as "utf8" or "123") are returned unchanged, so we
// only ever stem genuine lowercase English words.
//
// No external module dependency (stdlib only) — keeps the supply-chain surface
// at zero for this load-bearing retriever.

package main

// porterStemmer holds the mutable word buffer and the two cursors the classic
// algorithm threads through every step: k is the index of the final letter of
// the current word (b[0..k]); j is the scratch stem-end that ends()/setto()
// move as suffixes are matched and rewritten.
type porterStemmer struct {
	b []byte
	k int
	j int
}

// cons reports whether b[i] is a consonant. y is a consonant at position 0 or
// when preceded by a vowel, and a vowel when preceded by a consonant.
func (s *porterStemmer) cons(i int) bool {
	switch s.b[i] {
	case 'a', 'e', 'i', 'o', 'u':
		return false
	case 'y':
		if i == 0 {
			return true
		}
		return !s.cons(i - 1)
	default:
		return true
	}
}

// m returns the "measure" of b[0..j]: the number of vowel-consonant sequences,
// i.e. m in the pattern [C](VC){m}[V].
func (s *porterStemmer) m() int {
	n := 0
	i := 0
	for {
		if i > s.j {
			return n
		}
		if !s.cons(i) {
			break
		}
		i++
	}
	i++
	for {
		for {
			if i > s.j {
				return n
			}
			if s.cons(i) {
				break
			}
			i++
		}
		i++
		n++
		for {
			if i > s.j {
				return n
			}
			if !s.cons(i) {
				break
			}
			i++
		}
		i++
	}
}

// vowelinstem reports whether b[0..j] contains a vowel.
func (s *porterStemmer) vowelinstem() bool {
	for i := 0; i <= s.j; i++ {
		if !s.cons(i) {
			return true
		}
	}
	return false
}

// doublec reports whether b[i] and b[i-1] are the same consonant.
func (s *porterStemmer) doublec(i int) bool {
	if i < 1 {
		return false
	}
	if s.b[i] != s.b[i-1] {
		return false
	}
	return s.cons(i)
}

// cvc reports whether b[i-2..i] is consonant-vowel-consonant and the final
// consonant is not w, x, or y. Used to decide when to restore a trailing "e".
func (s *porterStemmer) cvc(i int) bool {
	if i < 2 || !s.cons(i) || s.cons(i-1) || !s.cons(i-2) {
		return false
	}
	switch s.b[i] {
	case 'w', 'x', 'y':
		return false
	}
	return true
}

// ends reports whether b[0..k] ends with suffix; when it does it sets j to the
// index of the last letter of the stem (k - len(suffix)).
func (s *porterStemmer) ends(suffix string) bool {
	l := len(suffix)
	if l > s.k+1 {
		return false
	}
	if string(s.b[s.k-l+1:s.k+1]) != suffix {
		return false
	}
	s.j = s.k - l
	return true
}

// setto replaces the suffix after j with s2 and resets k to the new end.
func (s *porterStemmer) setto(s2 string) {
	s.b = append(s.b[:s.j+1], s2...)
	s.k = s.j + len(s2)
}

// r (setto conditional on measure) rewrites the suffix only when m() > 0.
func (s *porterStemmer) r(s2 string) {
	if s.m() > 0 {
		s.setto(s2)
	}
}

// step1ab strips plurals and -ed/-ing (caresses->caress, ponies->poni,
// hopping->hop, plastered->plaster).
func (s *porterStemmer) step1ab() {
	if s.b[s.k] == 's' {
		switch {
		case s.ends("sses"):
			s.k -= 2
		case s.ends("ies"):
			s.setto("i")
		case s.b[s.k-1] != 's':
			s.k--
		}
	}
	if s.ends("eed") {
		if s.m() > 0 {
			s.k--
		}
	} else if (s.ends("ed") || s.ends("ing")) && s.vowelinstem() {
		s.k = s.j
		switch {
		case s.ends("at"):
			s.setto("ate")
		case s.ends("bl"):
			s.setto("ble")
		case s.ends("iz"):
			s.setto("ize")
		case s.doublec(s.k):
			s.k--
			switch s.b[s.k] {
			case 'l', 's', 'z':
				s.k++
			}
		default:
			if s.m() == 1 && s.cvc(s.k) {
				s.setto("e")
			}
		}
	}
}

// step1c turns a trailing y into i when the stem contains a vowel
// (happy->happi, but sky->sky).
func (s *porterStemmer) step1c() {
	if s.ends("y") && s.vowelinstem() {
		s.b[s.k] = 'i'
	}
}

// step2 maps double suffixes to single ones (relational->relate,
// digitizer->digitize) when m() > 0.
func (s *porterStemmer) step2() {
	if s.k < 1 {
		return
	}
	switch s.b[s.k-1] {
	case 'a':
		switch {
		case s.ends("ational"):
			s.r("ate")
		case s.ends("tional"):
			s.r("tion")
		}
	case 'c':
		switch {
		case s.ends("enci"):
			s.r("ence")
		case s.ends("anci"):
			s.r("ance")
		}
	case 'e':
		if s.ends("izer") {
			s.r("ize")
		}
	case 'l':
		switch {
		case s.ends("bli"): // DEPARTURE: reference uses bli->ble, not abli->able
			s.r("ble")
		case s.ends("alli"):
			s.r("al")
		case s.ends("entli"):
			s.r("ent")
		case s.ends("eli"):
			s.r("e")
		case s.ends("ousli"):
			s.r("ous")
		}
	case 'o':
		switch {
		case s.ends("ization"):
			s.r("ize")
		case s.ends("ation"):
			s.r("ate")
		case s.ends("ator"):
			s.r("ate")
		}
	case 's':
		switch {
		case s.ends("alism"):
			s.r("al")
		case s.ends("iveness"):
			s.r("ive")
		case s.ends("fulness"):
			s.r("ful")
		case s.ends("ousness"):
			s.r("ous")
		}
	case 't':
		switch {
		case s.ends("aliti"):
			s.r("al")
		case s.ends("iviti"):
			s.r("ive")
		case s.ends("biliti"):
			s.r("ble")
		}
	case 'g': // DEPARTURE: reference adds logi->log
		if s.ends("logi") {
			s.r("log")
		}
	}
}

// step3 deals with -ic-, -full, -ness etc. (triplicate->triplic,
// hopeful->hope) when m() > 0.
func (s *porterStemmer) step3() {
	switch s.b[s.k] {
	case 'e':
		switch {
		case s.ends("icate"):
			s.r("ic")
		case s.ends("ative"):
			s.r("")
		case s.ends("alize"):
			s.r("al")
		}
	case 'i':
		if s.ends("iciti") {
			s.r("ic")
		}
	case 'l':
		switch {
		case s.ends("ical"):
			s.r("ic")
		case s.ends("ful"):
			s.r("")
		}
	case 's':
		if s.ends("ness") {
			s.r("")
		}
	}
}

// step4 removes -ant, -ence, -er, -ic, -able, -ion (after s/t) etc. when the
// residual stem has measure m() > 1 (revival->reviv, adjustable->adjust).
func (s *porterStemmer) step4() {
	if s.k < 1 {
		return
	}
	matched := false
	switch s.b[s.k-1] {
	case 'a':
		matched = s.ends("al")
	case 'c':
		matched = s.ends("ance") || s.ends("ence")
	case 'e':
		matched = s.ends("er")
	case 'i':
		matched = s.ends("ic")
	case 'l':
		matched = s.ends("able") || s.ends("ible")
	case 'n':
		matched = s.ends("ant") || s.ends("ement") || s.ends("ment") || s.ends("ent")
	case 'o':
		if s.ends("ion") && (s.b[s.j] == 's' || s.b[s.j] == 't') {
			matched = true
		} else if s.ends("ou") {
			matched = true
		}
	case 's':
		matched = s.ends("ism")
	case 't':
		matched = s.ends("ate") || s.ends("iti")
	case 'u':
		matched = s.ends("ous")
	case 'v':
		matched = s.ends("ive")
	case 'z':
		matched = s.ends("ize")
	}
	if matched && s.m() > 1 {
		s.k = s.j
	}
}

// step5 removes a trailing e (probate->probat, but rate->rate) and undoubles a
// final l (controll->control) subject to the measure conditions.
func (s *porterStemmer) step5() {
	s.j = s.k
	if s.b[s.k] == 'e' {
		a := s.m()
		if a > 1 || (a == 1 && !s.cvc(s.k-1)) {
			s.k--
		}
	}
	if s.b[s.k] == 'l' && s.doublec(s.k) && s.m() > 1 {
		s.k--
	}
}

// porterStem returns the Porter stem of a single lowercase English word.
// Non-lowercase-ASCII tokens and words of length <= 2 are returned unchanged.
// tokenize() lowercases before calling, so callers there always pass a-z words.
func porterStem(word string) string {
	for i := 0; i < len(word); i++ {
		if word[i] < 'a' || word[i] > 'z' {
			return word // digits/symbols/uppercase: leave untouched
		}
	}
	if len(word) <= 2 {
		return word // DEPARTURE: never stem 1-2 letter words
	}
	s := &porterStemmer{b: []byte(word), k: len(word) - 1}
	s.step1ab()
	s.step1c()
	s.step2()
	s.step3()
	s.step4()
	s.step5()
	return string(s.b[:s.k+1])
}
