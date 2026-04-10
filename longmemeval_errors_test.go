// longmemeval_errors_test.go — Error analysis for LongMemEval misses.
// Dumps the questions that BM25 fails to retrieve at R@5 so we can
// categorize what dense embeddings / LLM rerank would fix.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

type MissedQuestion struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Question   string `json:"question"`
	TopRanked  string `json:"top_ranked_session"`
	CorrectIDs []string `json:"correct_ids"`
	CorrectRank int   `json:"correct_rank"` // -1 if not found in any rank
	InTop10    bool   `json:"in_top_10"`
}

func TestLongMemEval_ErrorAnalysis(t *testing.T) {
	questions, err := loadLME(lmeDataPath)
	if err != nil {
		t.Skipf("Skipping: %v", err)
	}

	var misses []MissedQuestion
	r5Hits, r10Hits, r10Only := 0, 0, 0

	for _, q := range questions {
		if len(q.HaystackSessions) == 0 || len(q.AnswerSessionIDs) == 0 {
			continue
		}

		docs, ids := buildSessionCorpus(q.HaystackSessions, q.HaystackSessionIDs)
		if len(docs) == 0 {
			continue
		}

		ranked := entityBoostedRank(docs, ids, q.Question)

		hitAt5 := recallAtK(ranked, q.AnswerSessionIDs, 5)
		hitAt10 := recallAtK(ranked, q.AnswerSessionIDs, 10)

		if hitAt5 {
			r5Hits++
		}
		if hitAt10 {
			r10Hits++
		}
		if hitAt10 && !hitAt5 {
			r10Only++
		}

		if !hitAt5 {
			// Find the actual rank of the correct session
			correctRank := -1
			correctSet := map[string]bool{}
			for _, c := range q.AnswerSessionIDs {
				correctSet[c] = true
			}
			for i, r := range ranked {
				if correctSet[r] {
					correctRank = i + 1
					break
				}
			}

			topRanked := ""
			if len(ranked) > 0 {
				topRanked = ranked[0]
			}

			misses = append(misses, MissedQuestion{
				ID:          q.QuestionID,
				Type:        q.QuestionType,
				Question:    q.Question,
				TopRanked:   topRanked,
				CorrectIDs:  q.AnswerSessionIDs,
				CorrectRank: correctRank,
				InTop10:     hitAt10,
			})
		}
	}

	t.Logf("\n=== Error Analysis ===")
	t.Logf("R@5 hits:  %d/500 (%.1f%%)", r5Hits, float64(r5Hits)*100/500)
	t.Logf("R@10 hits: %d/500 (%.1f%%)", r10Hits, float64(r10Hits)*100/500)
	t.Logf("In R@10 but not R@5: %d (rerank would promote these)", r10Only)
	t.Logf("Not in R@10 at all:  %d (need dense/semantic search)", len(misses)-r10Only)
	t.Logf("")
	t.Logf("Total misses at R@5: %d", len(misses))
	t.Logf("")

	// Categorize misses
	byType := map[string]int{}
	rerankaable := 0
	needsDense := 0
	for _, m := range misses {
		byType[m.Type]++
		if m.InTop10 {
			rerankaable++
		} else {
			needsDense++
		}
	}

	t.Logf("Misses by type:")
	for tp, count := range byType {
		t.Logf("  %-30s  %d misses", tp, count)
	}
	t.Logf("")
	t.Logf("Fixable by LLM rerank (in top-10): %d", rerankaable)
	t.Logf("Needs dense search (not in top-10): %d", needsDense)
	t.Logf("")

	t.Logf("Projected with soul-protocol + zvec:")
	// Conservative: rerank fixes 80% of rerankaable, dense fixes 60% of needsDense
	rerankFixed := int(float64(rerankaable) * 0.8)
	denseFixed := int(float64(needsDense) * 0.6)
	projected := r5Hits + rerankFixed + denseFixed
	t.Logf("  Rerank recovers: ~%d of %d", rerankFixed, rerankaable)
	t.Logf("  Dense recovers:  ~%d of %d", denseFixed, needsDense)
	t.Logf("  Projected R@5:   ~%.1f%% (%d/500)", float64(projected)*100/500, projected)
	t.Logf("")

	// Dump missed questions for manual inspection
	t.Logf("--- Missed questions (first 10) ---")
	limit := 10
	if len(misses) < limit {
		limit = len(misses)
	}
	for i := 0; i < limit; i++ {
		m := misses[i]
		rank := "NOT FOUND"
		if m.CorrectRank > 0 {
			rank = fmt.Sprintf("rank %d", m.CorrectRank)
		}
		t.Logf("  [%s] %s", m.Type, m.Question)
		t.Logf("    Correct at: %s | In top-10: %v", rank, m.InTop10)
		t.Logf("")
	}

	// Save full error report
	errFile := "benchmarks/longmemeval/error_analysis.json"
	data, _ := json.MarshalIndent(map[string]any{
		"total":       500,
		"r5_hits":     r5Hits,
		"r10_hits":    r10Hits,
		"r10_only":    r10Only,
		"needs_dense": needsDense,
		"misses":      misses,
	}, "", "  ")
	os.WriteFile(errFile, data, 0644)
	t.Logf("Full error report saved to %s", errFile)
}
