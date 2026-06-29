package resolver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestResolveViaAI(t *testing.T) {
	t.Run("returns suggested URL", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]interface{}{
				"content": []map[string]string{
					{"type": "text", "text": `{"fixedURL":"https://example.com/new","confidence":"low","reasoning":"redirects here"}`},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		result := types.ValidationResult{
			Link:   types.Link{URL: "https://old.example.com/page", Type: types.LinkTypeAbsolute},
			Valid:  false,
			Reason: types.ReasonHTTPError,
		}
		res := resolveViaAIWithURL(result, "test-key", srv.URL)
		if res.FixedURL != "https://example.com/new" {
			t.Errorf("FixedURL = %q", res.FixedURL)
		}
		if res.Strategy != types.StrategyAI {
			t.Errorf("Strategy = %q", res.Strategy)
		}
		if res.Confidence != types.ConfidenceLow {
			t.Errorf("Confidence = %q", res.Confidence)
		}
	})

	t.Run("null fixedURL returns unresolved", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := map[string]interface{}{
				"content": []map[string]string{
					{"type": "text", "text": `{"fixedURL":null,"confidence":"low","reasoning":"not found"}`},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer srv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://gone.example.com", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAIWithURL(result, "test-key", srv.URL)
		if res.FixedURL != "" {
			t.Errorf("expected empty FixedURL, got %q", res.FixedURL)
		}
	})

	t.Run("API error returns unresolved", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
		}))
		defer srv.Close()

		result := types.ValidationResult{
			Link:  types.Link{URL: "https://example.com", Type: types.LinkTypeAbsolute},
			Valid: false,
		}
		res := resolveViaAIWithURL(result, "test-key", srv.URL)
		if res.FixedURL != "" {
			t.Errorf("expected empty FixedURL on API error")
		}
	})
}
