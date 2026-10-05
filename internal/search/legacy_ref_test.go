// Reference copy of the v3-era BM25 pipeline (JSON index, articles listed in
// full), frozen as it was before the binary index, so parity tests compare
// the index-only search path against the code it replaced rather than against
// itself. Returns ids AND scores. Test-only; never called by production code.

package search

import (
	"math"
	"slices"
	"sort"
	"strings"

	"github.com/qbtrix/kb-go/internal/model"
)

type legacyIndex struct {
	docIDs        []string
	docLens       []int
	avgDL         float64
	postings      map[string][]Posting
	titleTokens   [][]string
	conceptTokens [][]string
}

func legacyBuild(articles []*model.WikiArticle) *legacyIndex {
	n := len(articles)
	si := &legacyIndex{
		docIDs: make([]string, n), docLens: make([]int, n), postings: map[string][]Posting{},
		titleTokens: make([][]string, n), conceptTokens: make([][]string, n),
	}
	total := 0
	for i, a := range articles {
		all := Tokenize(a.Title + " " + a.Summary + " " + a.Content +
			" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " "))
		si.docIDs[i] = a.ID
		si.docLens[i] = len(all)
		si.titleTokens[i] = Tokenize(a.Title)
		si.conceptTokens[i] = Tokenize(strings.Join(a.Concepts, " "))
		total += len(all)
		tfs := map[string]int{}
		for _, tok := range all {
			tfs[tok]++
		}
		for term, tf := range tfs {
			si.postings[term] = append(si.postings[term], Posting{i, tf})
		}
	}
	for _, pl := range si.postings {
		sort.Slice(pl, func(a, b int) bool { return pl[a][0] < pl[b][0] })
	}
	if n > 0 {
		si.avgDL = float64(total) / float64(n)
	}
	return si
}

func legacyPostingScores(queryTerms []string, si *legacyIndex) []float64 {
	nDocs := float64(len(si.docIDs))
	idfs := map[string]float64{}
	for _, term := range queryTerms {
		df := float64(len(si.postings[term]))
		idfs[term] = math.Log((nDocs-df+0.5)/(df+0.5) + 1)
	}
	scores := make([]float64, len(si.docIDs))
	for _, term := range queryTerms {
		idf := idfs[term]
		for _, p := range si.postings[term] {
			docIdx, tf := p[0], float64(p[1])
			dl := float64(si.docLens[docIdx])
			num := tf * (BM25K1 + 1)
			den := tf + BM25K1*(1-BM25B+BM25B*dl/si.avgDL)
			base := idf * num / den
			s := base
			if slices.Contains(si.titleTokens[docIdx], term) {
				s += base * 2.0
			}
			if slices.Contains(si.conceptTokens[docIdx], term) {
				s += base * 1.0
			}
			scores[docIdx] += s
		}
	}
	return scores
}

// legacyTokens is the slow path's per-article tokenization, computed once per
// corpus by the tests (the arithmetic below is verbatim v3).
type legacyTokens struct{ all, title, concept []string }

func legacyTokenize(a *model.WikiArticle) legacyTokens {
	return legacyTokens{
		all: Tokenize(a.Title + " " + a.Summary + " " + a.Content +
			" " + strings.Join(a.Concepts, " ") + " " + strings.Join(a.Categories, " ")),
		title:   Tokenize(a.Title),
		concept: Tokenize(strings.Join(a.Concepts, " ")),
	}
}

func legacySlowScores(toks []legacyTokens, queryTerms []string) []float64 {
	docs := make([][]string, len(toks))
	titleTokens := make([][]string, len(toks))
	conceptTokens := make([][]string, len(toks))
	totalLen := 0
	for i, tk := range toks {
		docs[i] = tk.all
		titleTokens[i] = tk.title
		conceptTokens[i] = tk.concept
		totalLen += len(docs[i])
	}
	avgDL := float64(totalLen) / float64(len(docs))
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
	scores := make([]float64, len(toks))
	for i, doc := range docs {
		s := 0.0
		dl := float64(len(doc))
		for _, term := range queryTerms {
			tf := float64(countStr(doc, term))
			num := tf * (BM25K1 + 1)
			den := tf + BM25K1*(1-BM25B+BM25B*dl/avgDL)
			base := idfs[term] * num / den
			s += base
			if slices.Contains(titleTokens[i], term) {
				s += base * 2.0
			}
			if slices.Contains(conceptTokens[i], term) {
				s += base * 1.0
			}
		}
		scores[i] = s
	}
	return scores
}

func legacyGlossary(articles []*model.WikiArticle, queryTerms []string, scores []float64) {
	for i := range articles {
		if articles[i].Kind != "glossary" {
			continue
		}
		matched := false
		termLower := porterStem(strings.ToLower(articles[i].Term))
		aliasesLower := make([]string, len(articles[i].Aliases))
		for k, al := range articles[i].Aliases {
			aliasesLower[k] = porterStem(strings.ToLower(al))
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
			scores[i] = (scores[i] + 1.0) * glossaryExactBoost
		}
	}
}

func legacyRank(articles []*model.WikiArticle, scores []float64, limit int) ([]string, []float64) {
	type scored struct {
		idx   int
		score float64
	}
	ranked := make([]scored, len(articles))
	for i, s := range scores {
		ranked[i] = scored{i, s}
	}
	sort.Slice(ranked, func(i, j int) bool { return ranked[i].score > ranked[j].score })
	var ids []string
	var out []float64
	for _, sc := range ranked {
		if sc.score <= 0 {
			break
		}
		ids = append(ids, articles[sc.idx].ID)
		out = append(out, sc.score)
		if len(ids) >= limit {
			break
		}
	}
	return ids, out
}

// legacyScope is one scope's v3 state: the full listing, its v3 index and
// per-article tokens.
type legacyScope struct {
	all  []*model.WikiArticle
	idx  *legacyIndex
	toks map[string]legacyTokens
}

func newLegacyScope(all []*model.WikiArticle) *legacyScope {
	ls := &legacyScope{all: all, idx: legacyBuild(all), toks: map[string]legacyTokens{}}
	for _, a := range all {
		ls.toks[a.ID] = legacyTokenize(a)
	}
	return ls
}

// search is the v3 `kb search` single-scope path over the scope's full
// listing: postings arithmetic for a full-scope search, slow path over the
// tag-filtered slice otherwise.
func (ls *legacyScope) search(query string, limit int, excludeTags string) ([]string, []float64) {
	articles := ls.all
	if excludeTags != "" {
		excluded := strings.Split(excludeTags, ",")
		var filtered []*model.WikiArticle
		for _, a := range ls.all {
			skip := false
			for _, tag := range excluded {
				if slices.Contains(a.Categories, strings.TrimSpace(tag)) {
					skip = true
					break
				}
			}
			if !skip {
				filtered = append(filtered, a)
			}
		}
		articles = filtered
	}
	if len(articles) == 0 || query == "" {
		return nil, nil
	}
	queryTerms := Tokenize(query)
	if len(queryTerms) == 0 {
		return nil, nil
	}
	var scores []float64
	if excludeTags == "" {
		scores = legacyPostingScores(queryTerms, ls.idx)
	} else {
		toks := make([]legacyTokens, len(articles))
		for i, a := range articles {
			toks[i] = ls.toks[a.ID]
		}
		scores = legacySlowScores(toks, queryTerms)
	}
	legacyGlossary(articles, queryTerms, scores)
	return legacyRank(articles, scores, limit)
}
