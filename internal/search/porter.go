// porter.go — stem, kb's token stemmer: step 1 of the Porter (1980) English
// stemmer, vendored and dependency-free.
//
// Every token kb indexes or queries goes through stem: Tokenize, the streaming
// index tokenizer (tokenStream.stem), and the glossary Term/Alias keys on both
// the build side (BuildIndex) and the boost side (applyGlossaryBoost). Index
// and query therefore always agree, so "opens" in a doc matches the query
// "open". The persisted index stores stemmed terms: changing what stem returns
// for any word means bumping IndexVersion (index.go).
//
// Step 1 only undoes inflection: 1a plurals (caresses->caress, ponies->poni,
// cats->cat), 1b -eed/-ed/-ing with Porter's at/bl/iz, double-consonant and
// cvc fix-ups (returning->return, shipped->ship, generating->generate,
// filing->file), 1c y->i after a vowel-bearing stem (happy/happies->happi).
// Porter's steps 2-5 (derivational suffixes and final -e) are deliberately not
// run: they merged unrelated words (generating/general, experiment/experience,
// use/us, one/on), which inflated document frequency and fired the title
// boost on lemmas, and paraphrased queries lost recall.
//
// Guards: words of length <= 2 and tokens containing anything other than a-z
// (digits, non-ASCII letters) are returned unchanged. Faithful to the
// reference implementation (https://tartarus.org/martin/PorterStemmer/)
// within step 1.

package search

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

// stem returns the step-1 Porter stem of a single lowercase English word.
// Non-lowercase-ASCII tokens and words of length <= 2 are returned unchanged.
// Tokenize() lowercases before calling, so callers there always pass a-z words.
func stem(word string) string {
	for i := 0; i < len(word); i++ {
		if word[i] < 'a' || word[i] > 'z' {
			return word // digits/symbols/non-ASCII: leave untouched
		}
	}
	if len(word) <= 2 {
		return word // never stem 1-2 letter words
	}
	s := &porterStemmer{b: []byte(word), k: len(word) - 1}
	s.step1ab()
	s.step1c()
	return string(s.b[:s.k+1])
}
