package resolver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func candidatesResponse(candidates []map[string]interface{}) map[string]interface{} {
	input, _ := json.Marshal(map[string]interface{}{"candidates": candidates})
	return map[string]interface{}{
		"content": []map[string]interface{}{
			{
				"type":  "tool_use",
				"name":  "report_candidates",
				"input": json.RawMessage(input),
			},
		},
	}
}

func TestResolveViaAI(t *testing.T) {
	t.Run("captures reasoning, numeric confidence, and all candidates", func(t *testing.T) {
		okSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
		}))
		defer okSrv.Close()
		deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
		}))
		defer deadSrv.Close()

		aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Unsorted on purpose: the 0.92 candidate is reachable and should win.
			_ = json.NewEncoder(w).Encode(candidatesResponse([]map[string]interface{}{
				{"url": deadSrv.URL, "confidence_score": 0.6, "reasoning": "dead one"},
				{"url": okSrv.URL, "confidence_score": 0.92, "reasoning": "the moved page"},
			}))
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://old.example.com/page", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "k", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})

		if res.FixedURL != okSrv.URL {
			t.Fatalf("FixedURL = %q, want %q", res.FixedURL, okSrv.URL)
		}
		if res.Reasoning != "the moved page" {
			t.Errorf("Reasoning = %q, want %q", res.Reasoning, "the moved page")
		}
		if res.ConfidenceScore != 0.92 {
			t.Errorf("ConfidenceScore = %v, want 0.92", res.ConfidenceScore)
		}
		if res.Confidence != types.ConfidenceHigh {
			t.Errorf("Confidence = %q, want high", res.Confidence)
		}
		// Both candidates should be retained, sorted best-first.
		if len(res.Candidates) != 2 {
			t.Fatalf("len(Candidates) = %d, want 2", len(res.Candidates))
		}
		if res.Candidates[0].ConfidenceScore < res.Candidates[1].ConfidenceScore {
			t.Error("candidates not sorted by confidence descending")
		}
		if !res.Candidates[0].Reachable || res.Candidates[1].Reachable {
			t.Errorf("reachability flags wrong: %+v", res.Candidates)
		}
	})

	t.Run("returns highest-confidence valid candidate", func(t *testing.T) {
		// Serve two candidates: high confidence returns 404, low confidence returns 200.
		// The resolver should try high first, fail, then accept low.
		lowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(200)
		}))
		defer lowSrv.Close()
		highSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
		}))
		defer highSrv.Close()

		aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Return low-confidence candidate first in the list — resolver must sort before validating.
			_ = json.NewEncoder(w).Encode(candidatesResponse([]map[string]interface{}{
				{"url": lowSrv.URL, "confidence_score": 0.4, "reasoning": "low"},
				{"url": highSrv.URL, "confidence_score": 0.9, "reasoning": "high"},
			}))
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:   types.Link{URL: "https://old.example.com/page", Text: "example", Type: types.LinkTypeAbsolute},
			Valid:  false,
			Reason: types.ReasonHTTPError,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "test-key", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})
		// High-confidence candidate (highSrv) returns 404, so low-confidence (lowSrv) should be accepted.
		if res.FixedURL != lowSrv.URL {
			t.Errorf("FixedURL = %q, want %q", res.FixedURL, lowSrv.URL)
		}
		if res.Strategy != types.StrategyAI {
			t.Errorf("Strategy = %q", res.Strategy)
		}
	})

	t.Run("all candidates fail HTTP — returns AI_NO_VALID_CANDIDATE", func(t *testing.T) {
		brokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
		}))
		defer brokenSrv.Close()

		aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(candidatesResponse([]map[string]interface{}{
				{"url": brokenSrv.URL + "/a", "confidence_score": 0.8, "reasoning": "a"},
				{"url": brokenSrv.URL + "/b", "confidence_score": 0.5, "reasoning": "b"},
			}))
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://gone.example.com", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "test-key", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})
		if res.FixedURL != "" {
			t.Errorf("expected empty FixedURL, got %q", res.FixedURL)
		}
		if res.UnresolvedReason != types.UnresolvedAINoValidCandidate {
			t.Errorf("UnresolvedReason = %q, want %q", res.UnresolvedReason, types.UnresolvedAINoValidCandidate)
		}
	})

	t.Run("empty candidates — returns AI_FAILED", func(t *testing.T) {
		aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(candidatesResponse(nil))
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://example.com/gone", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "test-key", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})
		if res.UnresolvedReason != types.UnresolvedAIFailed {
			t.Errorf("UnresolvedReason = %q, want AI_FAILED", res.UnresolvedReason)
		}
	})

	t.Run("malformed source URL skips API", func(t *testing.T) {
		called := false
		aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			w.WriteHeader(200)
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "not a url %%", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "test-key", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})
		if called {
			t.Error("API should not be called for malformed source URL")
		}
		if res.UnresolvedReason != types.UnresolvedSourceMalformed {
			t.Errorf("UnresolvedReason = %q, want SOURCE_MALFORMED", res.UnresolvedReason)
		}
	})

	t.Run("API error returns AI_FAILED", func(t *testing.T) {
		aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://example.com/page", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "test-key", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})
		if res.UnresolvedReason != types.UnresolvedAIFailed {
			t.Errorf("UnresolvedReason = %q, want AI_FAILED", res.UnresolvedReason)
		}
	})
}
