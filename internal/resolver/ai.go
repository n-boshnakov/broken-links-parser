package resolver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

const claudeAPIURL = "https://api.anthropic.com/v1/messages"

// aiResponse is the structured output we ask Claude to return.
type aiResponse struct {
	FixedURL   string `json:"fixedURL"`
	Confidence string `json:"confidence"`
	Reasoning  string `json:"reasoning"`
}

// ResolveViaAI asks Claude to suggest a replacement for an unresolved external link.
// Only called when --ai is set and all other strategies have failed.
func ResolveViaAI(result types.ValidationResult, apiKey string) types.ResolutionResult {
	return resolveViaAIWithURL(result, apiKey, claudeAPIURL)
}

func resolveViaAIWithURL(result types.ValidationResult, apiKey, apiURL string) types.ResolutionResult {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	prompt := fmt.Sprintf(
		"The following URL appears to be broken (returns an HTTP error): %s\n"+
			"It was found in a documentation file: %s\n\n"+
			"Please suggest the most likely correct replacement URL. "+
			"Respond ONLY with a JSON object: {\"fixedURL\": \"<url or null>\", \"confidence\": \"high|low\", \"reasoning\": \"<brief>\"}\n"+
			"If you cannot find a replacement, set fixedURL to null.",
		result.Link.URL, result.Link.SourceFile,
	)

	body := map[string]interface{}{
		"model":      "claude-haiku-4-5-20251001",
		"max_tokens": 256,
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
	}
	bodyBytes, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return types.ResolutionResult{ValidationResult: result}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return types.ResolutionResult{ValidationResult: result}
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		return types.ResolutionResult{ValidationResult: result}
	}

	// Extract text content from the Messages API response.
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &apiResp); err != nil || len(apiResp.Content) == 0 {
		return types.ResolutionResult{ValidationResult: result}
	}

	text := strings.TrimSpace(apiResp.Content[0].Text)
	// Extract the JSON object from the text.
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return types.ResolutionResult{ValidationResult: result}
	}
	text = text[start : end+1]

	var aiRes aiResponse
	if err := json.Unmarshal([]byte(text), &aiRes); err != nil || aiRes.FixedURL == "" || aiRes.FixedURL == "null" {
		return types.ResolutionResult{ValidationResult: result}
	}

	return types.ResolutionResult{
		ValidationResult: result,
		FixedURL:         aiRes.FixedURL,
		Strategy:         types.StrategyAI,
		Confidence:       types.ConfidenceLow,
	}
}
