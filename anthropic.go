// Built-in Anthropic Messages API client: kb's default compile path. A
// standalone user sets ANTHROPIC_API_KEY and runs `kb build`; the LLM writes
// the wiki at write time. It is used when no --compiler / KB_COMPILER is
// configured (those win, see compilerFromArgs in compile.go).
//
// ANTHROPIC_BASE_URL (the Anthropic SDK convention) overrides the endpoint
// root, default https://api.anthropic.com; requests go to <base>/v1/messages,
// so the client can run through a LiteLLM or other gateway that meters it.
//
// Token usage from each response is recorded on the article (usage.model,
// input_tokens, output_tokens). No cost is computed: kb carries no price
// table, and a stale one would report wrong numbers.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/qbtrix/kb-go/internal/model"
)

const (
	defaultModel   = "claude-haiku-4-5-20251001"
	defaultBaseURL = "https://api.anthropic.com"
	apiVersion     = "2023-06-01"
	apiTimeout     = 120 * time.Second
)

// anthropicBaseURL reads ANTHROPIC_BASE_URL, falling back to the public API.
func anthropicBaseURL() string {
	if b := strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")); b != "" {
		return b
	}
	return defaultBaseURL
}

// messagesURL is the Messages endpoint under a base URL.
func messagesURL(base string) string {
	if strings.TrimSpace(base) == "" {
		base = defaultBaseURL
	}
	return strings.TrimRight(base, "/") + "/v1/messages"
}

// callAnthropic sends one user prompt and returns the first text block plus
// the token usage. Any transport error, non-200 status or empty response is
// an error.
func callAnthropic(spec compilerSpec, system, prompt string) (string, *model.ArticleUsage, error) {
	modelName := spec.Model
	if modelName == "" {
		modelName = defaultModel
	}
	body, _ := json.Marshal(map[string]any{
		"model":      modelName,
		"max_tokens": 4096,
		"system":     system,
		"messages":   []map[string]string{{"role": "user", "content": prompt}},
	})

	req, err := http.NewRequest("POST", messagesURL(spec.BaseURL), bytes.NewReader(body))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("x-api-key", spec.APIKey)
	req.Header.Set("anthropic-version", apiVersion)
	req.Header.Set("content-type", "application/json")

	client := &http.Client{Timeout: apiTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("API request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("API error %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var apiResp struct {
		Model   string `json:"model"`
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
		return "", nil, fmt.Errorf("failed to parse API response: %w", err)
	}

	usedModel := apiResp.Model
	if usedModel == "" {
		usedModel = modelName
	}
	usage := &model.ArticleUsage{
		Model:        usedModel,
		InputTokens:  apiResp.Usage.InputTokens,
		OutputTokens: apiResp.Usage.OutputTokens,
	}

	for _, c := range apiResp.Content {
		if c.Type == "" || c.Type == "text" {
			return c.Text, usage, nil
		}
	}
	return "", usage, fmt.Errorf("empty API response")
}

// compileLLM compiles one source with the built-in client. compiled_with is
// the requested model (as in v0.3.0); usage carries the response's tokens.
func compileLLM(spec compilerSpec, rawText, source string, codeMod *CodeModule, terse bool) (*model.WikiArticle, error) {
	if strings.TrimSpace(spec.APIKey) == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY not set")
	}
	prompt := buildCompilePrompt(source, codeContextBlock(codeMod), rawText, terse)
	text, usage, err := callAnthropic(spec, "You are a knowledge compiler. Output only valid JSON. No markdown fences.", prompt)
	if err != nil {
		return nil, err
	}
	res, err := parseCompiledArticle([]byte(text))
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM output: %w", err)
	}
	modelName := spec.Model
	if modelName == "" {
		modelName = defaultModel
	}
	return newCompiledArticle(res, terse, modelName, usage), nil
}
