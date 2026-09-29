package resolver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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
			// Use specific (non-homepage) paths so they aren't down-ranked as generic.
			_ = json.NewEncoder(w).Encode(candidatesResponse([]map[string]interface{}{
				{"url": deadSrv.URL + "/dead", "confidence_score": 0.6, "reasoning": "dead one"},
				{"url": okSrv.URL + "/moved-page", "confidence_score": 0.92, "reasoning": "the moved page"},
			}))
		}))
		defer aiSrv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://old.example.com/page", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAnthropic(result, AIConfig{APIKey: "k", Model: defaultModel, BaseURL: aiSrv.URL}, WaybackContext{})

		if res.FixedURL != okSrv.URL+"/moved-page" {
			t.Fatalf("FixedURL = %q, want %q", res.FixedURL, okSrv.URL+"/moved-page")
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
			// Specific paths so they aren't down-ranked as generic homepages.
			_ = json.NewEncoder(w).Encode(candidatesResponse([]map[string]interface{}{
				{"url": lowSrv.URL + "/low", "confidence_score": 0.4, "reasoning": "low"},
				{"url": highSrv.URL + "/high", "confidence_score": 0.9, "reasoning": "high"},
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
		if res.FixedURL != lowSrv.URL+"/low" {
			t.Errorf("FixedURL = %q, want %q", res.FixedURL, lowSrv.URL+"/low")
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

func TestPickBestCandidate_GenericHandling(t *testing.T) {
	// Stub the candidate reachability check so no real network is used:
	// every URL is "reachable"; selection then depends only on generic-ness/order.
	orig := checkCandidate
	checkCandidate = func(string) bool { return true }
	defer func() { checkCandidate = orig }()

	mk := func(cands []map[string]interface{}) json.RawMessage {
		b, _ := json.Marshal(map[string]interface{}{"candidates": cands})
		return json.RawMessage(b)
	}
	base := types.ValidationResult{Link: types.Link{URL: "https://old.example.com/deep/page", Type: types.LinkTypeAbsolute}}

	t.Run("prefers specific page over higher-confidence homepage", func(t *testing.T) {
		raw := mk([]map[string]interface{}{
			{"url": "https://site.example.com/", "confidence_score": 0.95, "reasoning": "homepage"},
			{"url": "https://site.example.com/docs/the-page", "confidence_score": 0.6, "reasoning": "specific"},
		})
		res := pickBestCandidate(base, raw)
		if res.FixedURL != "https://site.example.com/docs/the-page" {
			t.Errorf("FixedURL = %q, want the specific page (not the homepage)", res.FixedURL)
		}
	})

	t.Run("falls back to homepage when it is the only reachable option", func(t *testing.T) {
		raw := mk([]map[string]interface{}{
			{"url": "https://site.example.com/", "confidence_score": 0.8, "reasoning": "homepage"},
		})
		res := pickBestCandidate(base, raw)
		if res.FixedURL != "https://site.example.com/" {
			t.Errorf("FixedURL = %q, want the homepage fallback", res.FixedURL)
		}
	})
}

// TestResolve_AIAuthWarnsOnce is the regression guard for the top-level AI-key
// warning: when the AI endpoint rejects the key (401), Resolve must emit exactly one
// run-level warning to stderr even across multiple affected links — not one per link,
// and not silence.
func TestResolve_AIAuthWarnsOnce(t *testing.T) {
	// AI endpoint that always rejects the key.
	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer aiSrv.Close()

	mkBroken := func(u string) types.ValidationResult {
		return types.ValidationResult{
			Link:       types.Link{URL: u, Type: types.LinkTypeAbsolute},
			Valid:      false,
			Reason:     types.ReasonHTTPError,
			StatusCode: 404,
		}
	}
	results := []types.ValidationResult{
		mkBroken("https://old.example.com/one"),
		mkBroken("https://old.example.com/two"),
	}
	opts := ResolveOptions{
		EnableAI: true,
		AI:       AIConfig{APIKey: "bad-key", Model: defaultModel, BaseURL: aiSrv.URL},
	}

	stderr := captureStderr(t, func() {
		out := Resolve(results, opts)
		// Each affected link still carries the per-row auth reason.
		for _, r := range out {
			if r.UnresolvedReason != types.UnresolvedAIAuthError {
				t.Errorf("link %s: UnresolvedReason = %q, want AI_AUTH_ERROR", r.Link.URL, r.UnresolvedReason)
			}
		}
	})

	if n := strings.Count(stderr, "AI resolution key was rejected"); n != 1 {
		t.Errorf("AI-key warning emitted %d times, want exactly 1\nstderr:\n%s", n, stderr)
	}
}

// captureStderr runs fn with os.Stderr redirected to a pipe and returns what was
// written. It restores the original stderr before returning.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		data, _ := io.ReadAll(r)
		done <- string(data)
	}()
	fn()
	_ = w.Close()
	os.Stderr = orig
	return <-done
}
