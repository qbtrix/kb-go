// LLM compilation: turns raw docs and parsed code into wiki articles through
// the Anthropic Messages API, with token accounting.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TokenUsage tracks API token consumption.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// buildCompilePrompt constructs the LLM compilation prompt shared by compileLLM and cmdPrepare.
// When terse is true, the prompt targets 120-180 words for agent context budgets.
// When terse is false, the prompt targets 400-800 words for full human documentation.
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

func compileLLM(rawText, source, model, apiKey string, codeMod *CodeModule, terse bool) (*WikiArticle, *TokenUsage, error) {
	if apiKey == "" {
		return nil, nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}

	var contextBlock string
	if codeMod != nil {
		contextBlock = fmt.Sprintf("\nAST-extracted structure:\n```\n%s```\n\n", formatCodeContext(codeMod))
	}

	prompt := buildCompilePrompt(source, contextBlock, rawText, terse)

	body, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"system":     "You are a knowledge compiler. Output only valid JSON. No markdown fences.",
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})

	req, err := http.NewRequest("POST", apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse Anthropic response with usage
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	usage := &TokenUsage{
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
	}

	if len(apiResp.Content) == 0 {
		return nil, usage, fmt.Errorf("empty API response")
	}

	text := apiResp.Content[0].Text
	// Strip markdown code fences if present
	text = strings.TrimPrefix(text, "```json")
	text = strings.TrimPrefix(text, "```")
	text = strings.TrimSuffix(text, "```")
	text = strings.TrimSpace(text)

	var result struct {
		Title      string   `json:"title"`
		Summary    string   `json:"summary"`
		Content    string   `json:"content"`
		Concepts   []string `json:"concepts"`
		Categories []string `json:"categories"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, usage, fmt.Errorf("failed to parse LLM output as JSON: %w\nraw: %s", err, text[:min(len(text), 200)])
	}

	slug := slugify(result.Title)
	now := time.Now().UTC().Format(time.RFC3339)

	// Set audience/depth/target_words based on terse mode.
	audience, depth, targetWords := "human", "deep", 500
	if terse {
		audience, depth, targetWords = "agent", "overview", 150
	}

	return &WikiArticle{
		ID:           slug,
		Title:        result.Title,
		Summary:      result.Summary,
		Content:      result.Content,
		Concepts:     nilToEmpty(result.Concepts),
		Categories:   nilToEmpty(result.Categories),
		SourceDocs:   nil,
		Backlinks:    nil,
		WordCount:    wordCount(result.Content),
		CompiledAt:   now,
		CompiledWith: model,
		Version:      1,
		Audience:     audience,
		Depth:        depth,
		TargetWords:  targetWords,
	}, usage, nil
}
