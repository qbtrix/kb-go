// longmemeval_test.go — LongMemEval benchmark harness for kb-go.
// Runs the 500-question LongMemEval benchmark using kb-go's BM25 search
// and the new convo entity extraction pipeline.
//
// Data file: benchmarks/longmemeval/longmemeval_s_cleaned.json (265MB, not committed)
// Download: https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned
//
// Run: go test -v -run TestLongMemEval -timeout 600s
// Skip: automatically skipped if data file is missing
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// --- LongMemEval data structures ---

type LMEQuestion struct {
	QuestionID        string          `json:"question_id"`
	QuestionType      string          `json:"question_type"`
	Question          string          `json:"question"`
	Answer            json.RawMessage `json:"answer"`
	QuestionDate      string          `json:"question_date"`
	HaystackDates     []string        `json:"haystack_dates"`
	HaystackSessionIDs []string       `json:"haystack_session_ids"`
	HaystackSessions  [][]LMETurn    `json:"haystack_sessions"`
	AnswerSessionIDs  []string        `json:"answer_session_ids"`
}

type LMETurn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// --- Benchmark core ---

// buildSessionCorpus concatenates user turns per session into one doc each.
// Returns (doc_texts, session_ids) aligned by index.
func buildSessionCorpus(sessions [][]LMETurn, sessionIDs []string) ([]string, []string) {
	var docs []string
	var ids []string
	for i, session := range sessions {
		var parts []string
		for _, turn := range session {
			if turn.Role == "user" {
				parts = append(parts, turn.Content)
			}
		}
		text := strings.Join(parts, " ")
		if text != "" {
			docs = append(docs, text)
			if i < len(sessionIDs) {
				ids = append(ids, sessionIDs[i])
			} else {
				ids = append(ids, fmt.Sprintf("session_%d", i))
			}
		}
	}
	return docs, ids
}

// bm25RankSessions scores each session doc against the query using BM25.
// Returns session IDs ranked by descending score.
func bm25RankSessions(docs []string, docIDs []string, query string) []string {
	queryTokens := tokenize(query)
	if len(queryTokens) == 0 || len(docs) == 0 {
		return nil
	}

	// Tokenize all docs
	tokenizedDocs := make([][]string, len(docs))
	totalLen := 0
	for i, doc := range docs {
		tokenizedDocs[i] = tokenize(doc)
		totalLen += len(tokenizedDocs[i])
	}
	avgDL := float64(totalLen) / float64(len(docs))

	// IDF
	idfs := map[string]float64{}
	for _, term := range queryTokens {
		df := 0
		for _, doc := range tokenizedDocs {
			if containsStr(doc, term) {
				df++
			}
		}
		n := float64(len(docs))
		idfs[term] = ln((n-float64(df)+0.5)/(float64(df)+0.5) + 1)
	}

	// Score
	type scored struct {
		id    string
		score float64
	}
	results := make([]scored, len(docs))
	for i, doc := range tokenizedDocs {
		s := 0.0
		dl := float64(len(doc))
		for _, term := range queryTokens {
			tf := float64(countStr(doc, term))
			num := tf * (bm25K1 + 1)
			den := tf + bm25K1*(1-bm25B+bm25B*dl/avgDL)
			s += idfs[term] * num / den
		}
		results[i] = scored{id: docIDs[i], score: s}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	ranked := make([]string, len(results))
	for i, r := range results {
		ranked[i] = r.id
	}
	return ranked
}

// entityBoostedRank runs entity extraction on the query, boosts sessions
// that mention the same entities, then falls back to BM25.
func entityBoostedRank(docs []string, docIDs []string, query string) []string {
	queryEntities := extractEntities(query)

	if len(queryEntities) == 0 {
		return bm25RankSessions(docs, docIDs, query)
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 || len(docs) == 0 {
		return nil
	}

	// Tokenize docs
	tokenizedDocs := make([][]string, len(docs))
	totalLen := 0
	for i, doc := range docs {
		tokenizedDocs[i] = tokenize(doc)
		totalLen += len(tokenizedDocs[i])
	}
	avgDL := float64(totalLen) / float64(len(docs))

	// IDF
	idfs := map[string]float64{}
	for _, term := range queryTokens {
		df := 0
		for _, doc := range tokenizedDocs {
			if containsStr(doc, term) {
				df++
			}
		}
		n := float64(len(docs))
		idfs[term] = ln((n-float64(df)+0.5)/(float64(df)+0.5) + 1)
	}

	// Entity tokens for boosting
	entityTokenSet := map[string]bool{}
	for _, e := range queryEntities {
		for _, tok := range tokenize(e.Name) {
			entityTokenSet[tok] = true
		}
	}

	// Score with entity boost
	type scored struct {
		id    string
		score float64
	}
	results := make([]scored, len(docs))
	for i, doc := range tokenizedDocs {
		bm25Score := 0.0
		dl := float64(len(doc))
		for _, term := range queryTokens {
			tf := float64(countStr(doc, term))
			num := tf * (bm25K1 + 1)
			den := tf + bm25K1*(1-bm25B+bm25B*dl/avgDL)
			bm25Score += idfs[term] * num / den
		}

		// Entity boost: 2x for docs containing query entities
		entityBoost := 0.0
		for _, tok := range doc {
			if entityTokenSet[tok] {
				entityBoost += 1.0
			}
		}
		if entityBoost > 0 {
			bm25Score *= (1.0 + entityBoost*0.5)
		}

		results[i] = scored{id: docIDs[i], score: bm25Score}
	}

	sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })

	ranked := make([]string, len(results))
	for i, r := range results {
		ranked[i] = r.id
	}
	return ranked
}

func ln(x float64) float64 {
	if x <= 0 {
		return 0
	}
	// Use the same log as BM25 in kb.go
	result := 0.0
	for x >= 2 {
		result += 0.6931471805599453 // ln(2)
		x /= 2
	}
	// Taylor series for ln(1+y) where y = x-1, |y| < 1
	y := x - 1
	term := y
	for i := 1; i <= 20; i++ {
		if i%2 == 1 {
			result += term / float64(i)
		} else {
			result -= term / float64(i)
		}
		term *= y
	}
	return result
}

// recallAtK checks if ANY of the correct session IDs appear in the top-K ranked results.
func recallAtK(ranked []string, correct []string, k int) bool {
	if k > len(ranked) {
		k = len(ranked)
	}
	correctSet := map[string]bool{}
	for _, c := range correct {
		correctSet[c] = true
	}
	for _, r := range ranked[:k] {
		if correctSet[r] {
			return true
		}
	}
	return false
}

// --- Test runner ---

const lmeDataPath = "benchmarks/longmemeval/longmemeval_s_cleaned.json"
const lmeOraclePath = "benchmarks/longmemeval/longmemeval_oracle.json"
const lmeRankingsOutput = "benchmarks/longmemeval/go_bm25_rankings.json"

func loadLME(path string) ([]LMEQuestion, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var questions []LMEQuestion
	if err := json.Unmarshal(data, &questions); err != nil {
		return nil, err
	}
	return questions, nil
}

func TestLongMemEval_BM25_Oracle(t *testing.T) {
	questions, err := loadLME(lmeOraclePath)
	if err != nil {
		t.Skipf("Skipping: %v (download from HuggingFace: xiaowu0162/longmemeval-cleaned)", err)
	}

	runBenchmark(t, "BM25-Oracle", questions, bm25RankSessions)
}

func TestLongMemEval_EntityBoosted_Oracle(t *testing.T) {
	questions, err := loadLME(lmeOraclePath)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	runBenchmark(t, "EntityBoosted-Oracle", questions, entityBoostedRank)
}

func TestLongMemEval_BM25_Small(t *testing.T) {
	questions, err := loadLME(lmeDataPath)
	if err != nil {
		t.Skipf("Skipping: %v (download longmemeval_s_cleaned.json from HuggingFace)", err)
	}

	runBenchmark(t, "BM25-Small", questions, bm25RankSessions)
}

// TestLongMemEval_ExportRankings exports Go BM25 top-15 per question as JSON
// for consumption by the Python benchmark harness (soul rerank, zvec dense).
func TestLongMemEval_ExportRankings(t *testing.T) {
	questions, err := loadLME(lmeDataPath)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	type QuestionRanking struct {
		QuestionID  string   `json:"question_id"`
		Question    string   `json:"question"`
		QType       string   `json:"question_type"`
		CorrectIDs  []string `json:"correct_ids"`
		BM25Top15   []string `json:"bm25_top15"`
		SessionDocs map[string]string `json:"session_docs"`
	}

	var rankings []QuestionRanking

	for _, q := range questions {
		if len(q.HaystackSessions) == 0 || len(q.AnswerSessionIDs) == 0 {
			continue
		}

		docs, ids := buildSessionCorpus(q.HaystackSessions, q.HaystackSessionIDs)
		if len(docs) == 0 {
			continue
		}

		ranked := entityBoostedRank(docs, ids, q.Question)
		top15 := ranked
		if len(top15) > 15 {
			top15 = top15[:15]
		}

		// Include session docs for the top-15 (for zvec embedding in Python)
		docMap := map[string]string{}
		idToDoc := make(map[string]string)
		for i, id := range ids {
			idToDoc[id] = docs[i]
		}
		for _, sid := range top15 {
			docMap[sid] = idToDoc[sid]
		}

		rankings = append(rankings, QuestionRanking{
			QuestionID: q.QuestionID,
			Question:   q.Question,
			QType:      q.QuestionType,
			CorrectIDs: q.AnswerSessionIDs,
			BM25Top15:  top15,
			SessionDocs: docMap,
		})
	}

	data, _ := json.MarshalIndent(rankings, "", "  ")
	os.WriteFile(lmeRankingsOutput, data, 0644)
	t.Logf("Exported %d question rankings to %s", len(rankings), lmeRankingsOutput)
}

func TestLongMemEval_EntityBoosted_Small(t *testing.T) {
	questions, err := loadLME(lmeDataPath)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	runBenchmark(t, "EntityBoosted-Small", questions, entityBoostedRank)
}

type rankerFunc func(docs []string, docIDs []string, query string) []string

func runBenchmark(t *testing.T, name string, questions []LMEQuestion, ranker rankerFunc) {
	t.Helper()

	ks := []int{1, 3, 5, 10}
	hits := make(map[int]int) // k -> hit count
	typeHits := make(map[string]map[int]int) // type -> k -> hits
	typeCounts := make(map[string]int)

	start := time.Now()
	skipped := 0

	for i, q := range questions {
		if len(q.HaystackSessions) == 0 || len(q.AnswerSessionIDs) == 0 {
			skipped++
			continue
		}

		docs, ids := buildSessionCorpus(q.HaystackSessions, q.HaystackSessionIDs)
		if len(docs) == 0 {
			skipped++
			continue
		}

		ranked := ranker(docs, ids, q.Question)

		for _, k := range ks {
			if recallAtK(ranked, q.AnswerSessionIDs, k) {
				hits[k]++
				if typeHits[q.QuestionType] == nil {
					typeHits[q.QuestionType] = map[int]int{}
				}
				typeHits[q.QuestionType][k]++
			}
		}
		typeCounts[q.QuestionType]++

		if (i+1)%100 == 0 {
			t.Logf("  [%d/%d] R@5=%.1f%%", i+1, len(questions),
				float64(hits[5])*100/float64(i+1-skipped))
		}
	}

	elapsed := time.Since(start)
	evaluated := len(questions) - skipped

	t.Logf("\n=== %s Results ===", name)
	t.Logf("Questions: %d evaluated, %d skipped, %.1fs elapsed", evaluated, skipped, elapsed.Seconds())
	t.Logf("")

	for _, k := range ks {
		pct := float64(hits[k]) * 100 / float64(evaluated)
		t.Logf("  Recall@%-3d  %5.1f%%  (%d/%d)", k, pct, hits[k], evaluated)
	}

	t.Logf("")
	t.Logf("By question type:")

	// Sort types for consistent output
	var types []string
	for tp := range typeCounts {
		types = append(types, tp)
	}
	sort.Strings(types)

	for _, tp := range types {
		count := typeCounts[tp]
		r5 := 0
		if typeHits[tp] != nil {
			r5 = typeHits[tp][5]
		}
		pct := float64(r5) * 100 / float64(count)
		t.Logf("  %-30s  R@5=%5.1f%%  (%d/%d)", tp, pct, r5, count)
	}
}
