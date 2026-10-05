// Tokenization and BM25 ranking over wiki articles (k1=1.2, b=0.75), with
// title, concept and glossary boosts.

package search

import (
	"math"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/qbtrix/kb-go/internal/model"
)

// Tokenize lowercases, splits on non-alphanumeric runes, and stems each token
// (Porter step 1, see porter.go). Stemming is the SINGLE shared step that makes
// BM25 match inflected variants: because both the index (BuildIndex) and the
// query (BM25WithIndex) Tokenize through here, "opens" in a doc and the query
// "open" both reduce to "open" and match.
//
// The persisted index stores these stemmed tokens, so changing stem means
// bumping IndexVersion: files written under another stemmer are ignored and
// healed. stem leaves digits and <=2-letter tokens untouched.
func Tokenize(text string) []string {
	lower := strings.ToLower(text)
	splitter := func(c rune) bool {
		return !unicode.IsLetter(c) && !unicode.IsDigit(c)
	}
	fields := strings.FieldsFunc(lower, splitter)
	tokens := make([]string, len(fields))
	for i, f := range fields {
		tokens[i] = stem(f)
	}
	return tokens
}

func BM25(articles []*model.WikiArticle, query string, limit int) []*model.WikiArticle {
	return BM25WithIndex(articles, query, limit, nil)
}

// glossaryExactBoost multiplies (and baselines) the score of a glossary
// article whose Term or an Alias exactly matches a query term — see issue #15.
const glossaryExactBoost = 10.0

func BM25WithIndex(articles []*model.WikiArticle, query string, limit int, si *Index) []*model.WikiArticle {
	if len(articles) == 0 || query == "" {
		return nil
	}

	queryTerms := Tokenize(query)
	if len(queryTerms) == 0 {
		return nil
	}

	var scores []float64
	if IndexMatches(si, articles) {
		scores = bm25ScoresFromPostings(queryTerms, si)
	} else {
		scores = bm25ScoresSlow(articles, queryTerms)
	}

	applyGlossaryBoost(articles, queryTerms, scores)
	return rankByScore(articles, scores, limit)
}

// bm25ScoresFromPostings computes BM25 + title/concept-boost scores reading
// ONLY the query terms' postings lists: df is the postings length, tf comes
// from the postings entries. Documents that contain none of the query terms
// are never touched, so cost scales with matching docs, not corpus size.
// Semantics match bm25ScoresSlow exactly — a term absent from a doc
// contributes base = 0 there (tf = 0), and a term present in a doc's title or
// concepts is by construction in that doc's postings (the "all" token stream
// includes title and concepts), so boosts apply to the same docs.
func bm25ScoresFromPostings(queryTerms []string, si *Index) []float64 {
	nDocs := float64(len(si.DocIDs))

	idfs := map[string]float64{}
	for _, term := range queryTerms {
		df := float64(len(si.postingsFor(term)))
		idfs[term] = math.Log((nDocs-df+0.5)/(df+0.5) + 1)
	}

	scores := make([]float64, len(si.DocIDs))
	for _, term := range queryTerms {
		idf := idfs[term]
		for _, p := range si.postingsFor(term) {
			docIdx, tf := p[0], float64(p[1])
			dl := float64(si.DocLens[docIdx])
			num := tf * (BM25K1 + 1)
			den := tf + BM25K1*(1-BM25B+BM25B*dl/si.AvgDL)
			base := idf * num / den
			s := base
			// Title boost: 3x for terms appearing in the title
			if slices.Contains(si.TitleTokens[docIdx], term) {
				s += base * 2.0
			}
			// Concept boost: 2x for terms matching concepts
			if slices.Contains(si.ConceptTokens[docIdx], term) {
				s += base * 1.0
			}
			scores[docIdx] += s
		}
	}
	return scores
}

// bm25ScoresSlow tokenizes every article on the fly and scores it. Used when
// no (matching, current-format) search index is available.
func bm25ScoresSlow(articles []*model.WikiArticle, queryTerms []string) []float64 {
	docs := make([][]string, len(articles))
	titleTokens := make([][]string, len(articles))
	conceptTokens := make([][]string, len(articles))
	totalLen := 0
	for i, a := range articles {
		docs[i] = Tokenize(a.Title + " " + a.Summary + " " + a.Content +
			" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
		titleTokens[i] = Tokenize(a.Title)
		conceptTokens[i] = Tokenize(strings.Join(a.Concepts, " "))
		totalLen += len(docs[i])
	}
	avgDL := float64(totalLen) / float64(len(docs))

	// IDF per query term
	idfs := map[string]float64{}
	for _, term := range queryTerms {
		df := 0
		for _, doc := range docs {
			if slices.Contains(doc, term) {
				df++
			}
		}
		idfs[term] = math.Log((float64(len(docs))-float64(df)+0.5)/(float64(df)+0.5) + 1)
	}

	// Score each doc with title (3x) and concept (2x) boosting.
	scores := make([]float64, len(articles))
	for i, doc := range docs {
		s := 0.0
		dl := float64(len(doc))
		for _, term := range queryTerms {
			tf := float64(countStr(doc, term))
			num := tf * (BM25K1 + 1)
			den := tf + BM25K1*(1-BM25B+BM25B*dl/avgDL)
			base := idfs[term] * num / den
			s += base

			// Title boost: 3x for terms appearing in the title
			if slices.Contains(titleTokens[i], term) {
				s += base * 2.0
			}
			// Concept boost: 2x for terms matching concepts
			if slices.Contains(conceptTokens[i], term) {
				s += base * 1.0
			}
		}
		scores[i] = s
	}
	return scores
}

// applyGlossaryBoost applies the glossary exact-Term / Alias boost
// (case-insensitive), at most once per document. It both adds a baseline (so
// alias-only matches with zero organic BM25 still rank) and multiplies the
// result, so a glossary hit consistently outranks mention-heavy module
// articles. Runs over the articles slice — glossary metadata lives on the
// articles, not in the search index — and is shared by both scoring paths.
func applyGlossaryBoost(articles []*model.WikiArticle, queryTerms []string, scores []float64) {
	for i := range articles {
		if articles[i].Kind != "glossary" {
			continue
		}
		matched := false
		// queryTerms are stemmed (via Tokenize), so the Term/Alias sides are
		// stemmed too or a stemmed query token could never equal a raw term.
		// Identical raw inputs stem identically, so every exact hit survives,
		// and variants like alias "opens" vs query "open" also match. stem
		// leaves multi-word terms (containing a space) untouched.
		termLower := stem(strings.ToLower(articles[i].Term))
		aliasesLower := make([]string, len(articles[i].Aliases))
		for k, al := range articles[i].Aliases {
			aliasesLower[k] = stem(strings.ToLower(al))
		}
		for _, qt := range queryTerms {
			qLower := strings.ToLower(qt)
			if termLower != "" && termLower == qLower {
				matched = true
				break
			}
			for _, al := range aliasesLower {
				if al == qLower {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			// Add a baseline so alias-only matches (TF=0 in docs) still rank
			// positive, then multiply so we dominate any module article that
			// merely mentions the term in body text.
			scores[i] = (scores[i] + 1.0) * glossaryExactBoost
		}
	}
}

// rankByScore sorts descending and returns up to limit articles with a
// strictly positive score.
func rankByScore(articles []*model.WikiArticle, scores []float64, limit int) []*model.WikiArticle {
	var result []*model.WikiArticle
	for _, i := range rankPositions(scores, limit) {
		result = append(result, articles[i])
	}
	return result
}

// rankPositions is the one ranking rule every BM25 path shares: positions in
// scores sorted by descending score (sort.Slice over the whole slice, so ties
// fall where they always have), cut at the first non-positive score and after
// limit entries. As before, limit <= 0 still yields one hit.
func rankPositions(scores []float64, limit int) []int {
	type scored struct {
		idx   int
		score float64
	}
	ranked := make([]scored, len(scores))
	for i, s := range scores {
		ranked[i] = scored{i, s}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })

	var result []int
	for _, sc := range ranked {
		if sc.score <= 0 {
			break
		}
		result = append(result, sc.idx)
		if len(result) >= limit {
			break
		}
	}
	return result
}

func countStr(tokens []string, term string) int {
	n := 0
	for _, t := range tokens {
		if t == term {
			n++
		}
	}
	return n
}

const (
	BM25K1 = 1.2
	BM25B  = 0.75
)
