package resolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
	"github.com/n-boshnakov/broken-links-parser/internal/validator"
)

const (
	anthropicAPIURL   = "https://api.anthropic.com/v1/messages"
	openaiChatPath    = "/v1/chat/completions"
	defaultModel      = "claude-haiku-4-5-20251001"
	defaultOpenAIModel = "gpt-4o-mini"
)

// aiCandidate is one suggestion returned by the AI.
type aiCandidate struct {
	URL        string  `json:"url"`
	Confidence float64 `json:"confidence_score"` // 0.0–1.0; higher = more confident
	Reasoning  string  `json:"reasoning"`
}

// AIConfig holds the resolved AI provider settings.
type AIConfig struct {
	APIKey  string
	BaseURL string // empty = use Anthropic native API
	Model   string
}

// AIConfigFromEnv reads AI_API_KEY, AI_BASE_URL, AI_MODEL from the environment.
func AIConfigFromEnv() AIConfig {
	cfg := AIConfig{
		APIKey:  os.Getenv("AI_API_KEY"),
		BaseURL: os.Getenv("AI_BASE_URL"),
		Model:   os.Getenv("AI_MODEL"),
	}
	if cfg.Model == "" {
		if cfg.BaseURL != "" {
			cfg.Model = defaultOpenAIModel
		} else {
			cfg.Model = defaultModel
		}
	}
	return cfg
}

// isOpenAICompatible returns true when the base URL points to a non-Anthropic endpoint.
// The Anthropic path also accepts a BaseURL override for testing, so we check explicitly.
func (c AIConfig) isOpenAICompatible() bool {
	return c.BaseURL != "" && c.BaseURL != anthropicAPIURL
}

// ResolveViaAI asks the configured AI to suggest replacements for an unresolved external link.
func ResolveViaAI(result types.ValidationResult, cfg AIConfig, wctx WaybackContext) types.ResolutionResult {
	if cfg.isOpenAICompatible() {
		return resolveViaOpenAI(result, cfg, wctx)
	}
	return resolveViaAnthropic(result, cfg, wctx)
}

// ── Anthropic native API ──────────────────────────────────────────────────────

func resolveViaAnthropic(result types.ValidationResult, cfg AIConfig, wctx WaybackContext) types.ResolutionResult {
	// ponytail: BaseURL may be overridden in tests; production always uses anthropicAPIURL
	apiURL := anthropicAPIURL
	if cfg.BaseURL != "" {
		apiURL = cfg.BaseURL
	}

	if _, err := url.ParseRequestURI(result.Link.URL); err != nil {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedSourceMalformed}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	body := map[string]interface{}{
		"model":       cfg.Model,
		"max_tokens":  512,
		"tools":       []map[string]interface{}{candidateTool()},
		"tool_choice": map[string]string{"type": "auto"},
		"messages":    []map[string]string{{"role": "user", "content": buildPrompt(result.Link, wctx)}},
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	data, reason := doAIRequest(req)
	if reason != "" {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: reason}
	}

	var apiResp struct {
		Content []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
	}
	for _, block := range apiResp.Content {
		if block.Type == "tool_use" && block.Name == "report_candidates" {
			return pickBestCandidate(result, block.Input)
		}
	}
	return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
}

// ── OpenAI-compatible API (LiteLLM, Azure, etc.) ─────────────────────────────

func resolveViaOpenAI(result types.ValidationResult, cfg AIConfig, wctx WaybackContext) types.ResolutionResult {
	if _, err := url.ParseRequestURI(result.Link.URL); err != nil {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedSourceMalformed}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Convert the Anthropic tool schema to OpenAI function format.
	tool := candidateTool()
	body := map[string]interface{}{
		"model": cfg.Model,
		"tools": []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        tool["name"],
					"description": tool["description"],
					"parameters":  tool["input_schema"],
				},
			},
		},
		"tool_choice": "required",
		"messages":    []map[string]string{{"role": "user", "content": buildPrompt(result.Link, wctx)}},
	}
	bodyBytes, _ := json.Marshal(body)

	endpoint := strings.TrimRight(cfg.BaseURL, "/") + openaiChatPath
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	data, reason := doAIRequest(req)
	if reason != "" {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: reason}
	}

	// Parse OpenAI tool_calls response.
	var apiResp struct {
		Choices []struct {
			Message struct {
				ToolCalls []struct {
					Function struct {
						Name      string          `json:"name"`
						Arguments json.RawMessage `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil || len(apiResp.Choices) == 0 {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
	}
	for _, tc := range apiResp.Choices[0].Message.ToolCalls {
		if tc.Function.Name == "report_candidates" {
			// Arguments may be a JSON string (escaped) or a raw JSON object depending on the provider.
			args := tc.Function.Arguments
			var s string
			if json.Unmarshal(args, &s) == nil {
				// It was a JSON-encoded string; use the inner content.
				args = json.RawMessage(s)
			}
			return pickBestCandidate(result, args)
		}
	}
	return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
}

// ── Shared helpers ────────────────────────────────────────────────────────────

func doAIRequest(req *http.Request) ([]byte, string) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, types.UnresolvedAIFailed
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.UnresolvedAIFailed
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return data, ""
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, types.UnresolvedAIAuthError
	default:
		return nil, types.UnresolvedAIFailed
	}
}

// candidateTool returns the shared tool/function schema used by both providers.
func candidateTool() map[string]interface{} {
	return map[string]interface{}{
		"name":        "report_candidates",
		"description": "Report up to 3 candidate replacement URLs for a broken link",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"candidates": map[string]interface{}{
					"type":        "array",
					"description": "Candidate replacement URLs. Leave empty if none found.",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"url":              map[string]interface{}{"type": "string"},
							"confidence_score": map[string]interface{}{"type": "number", "description": "0.0–1.0"},
							"reasoning":        map[string]interface{}{"type": "string"},
						},
						"required": []string{"url", "confidence_score", "reasoning"},
					},
					"maxItems": 3,
				},
			},
			"required": []string{"candidates"},
		},
	}
}

func pickBestCandidate(result types.ValidationResult, raw json.RawMessage) types.ResolutionResult {
	var input struct {
		Candidates []aiCandidate `json:"candidates"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || len(input.Candidates) == 0 {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAIFailed}
	}

	sort.Slice(input.Candidates, func(i, j int) bool {
		return input.Candidates[i].Confidence > input.Candidates[j].Confidence
	})

	for _, c := range input.Candidates {
		if c.URL == "" {
			continue
		}
		if _, err := url.ParseRequestURI(c.URL); err != nil {
			continue
		}
		// Reject Wayback Machine URLs — the AI sometimes suggests archive.org as a
		// "replacement" but those are archive/search pages, not live content.
		if strings.Contains(c.URL, "web.archive.org") || strings.Contains(c.URL, "archive.org/web") {
			continue
		}
		if validator.CheckURL(c.URL) {
			confidence := types.ConfidenceLow
			if c.Confidence >= 0.7 {
				confidence = types.ConfidenceHigh
			}
			return types.ResolutionResult{
				ValidationResult: result,
				FixedURL:         c.URL,
				Strategy:         types.StrategyAI,
				Confidence:       confidence,
			}
		}
	}
	return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedAINoValidCandidate}
}

func buildPrompt(link types.Link, wctx WaybackContext) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("The following URL returns an HTTP error when accessed: %s\n", link.URL))
	if link.Text != "" {
		sb.WriteString(fmt.Sprintf("The link text is: %q\n", link.Text))
	}
	sb.WriteString(fmt.Sprintf("It appears in the documentation file: %s\n", link.SourceFile))
	if wctx.Title != "" {
		sb.WriteString(fmt.Sprintf("An archived version of this page had the title: %q\n", wctx.Title))
	}
	if wctx.Excerpt != "" {
		sb.WriteString(fmt.Sprintf("Its content began: %s\n", wctx.Excerpt))
	}
	if wctx.ParentURL != "" {
		sb.WriteString(fmt.Sprintf("The parent site is still live at %s — the content may have moved there.\n", wctx.ParentURL))
	}
	sb.WriteString("\nFind the current URL for this specific content — not a homepage or general page on the same domain. ")
	sb.WriteString("The replacement should point to the same topic or resource as the original link. ")
	sb.WriteString("You must call the report_candidates tool with up to 3 candidates, each with a confidence score (0.0–1.0). ")
	sb.WriteString("If you have no specific candidates, call report_candidates with an empty array — do not suggest generic homepages or archive URLs.")
	return sb.String()
}
