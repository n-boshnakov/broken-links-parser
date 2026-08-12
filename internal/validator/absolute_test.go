package validator

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func absLink(url string) types.Link {
	return types.Link{URL: url, Type: types.LinkTypeAbsolute, SourceFile: "test.md"}
}

func TestValidateAbsolute(t *testing.T) {
	// 200 HEAD
	t.Run("200 HEAD", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if !res.Valid || res.StatusCode != 200 {
			t.Errorf("got valid=%v status=%d", res.Valid, res.StatusCode)
		}
	})

	// 405 HEAD → 200 GET
	t.Run("405 HEAD fallback GET 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(405)
				return
			}
			w.WriteHeader(200)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if !res.Valid || res.StatusCode != 200 {
			t.Errorf("got valid=%v status=%d", res.Valid, res.StatusCode)
		}
	})

	// 404
	t.Run("404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(404)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if res.Valid || res.StatusCode != 404 || res.Reason != types.ReasonHTTPError {
			t.Errorf("got valid=%v status=%d reason=%q", res.Valid, res.StatusCode, res.Reason)
		}
	})

	// 500
	t.Run("500", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(500)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if res.Valid || res.StatusCode != 500 {
			t.Errorf("got valid=%v status=%d", res.Valid, res.StatusCode)
		}
	})

	// 301 redirect → 200
	t.Run("301 redirect to 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/old" {
				http.Redirect(w, r, "/new", http.StatusMovedPermanently)
				return
			}
			w.WriteHeader(200)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL+"/old"), srv.Client(), nil, nil)
		if !res.Valid {
			t.Errorf("expected valid after redirect, got valid=%v status=%d", res.Valid, res.StatusCode)
		}
	})

	// 301 redirect → 404: the final status must be classified (confirms the client
	// follows redirects, so no manual redirect-following code is needed).
	t.Run("301 redirect to 404", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/old" {
				http.Redirect(w, r, "/gone", http.StatusMovedPermanently)
				return
			}
			w.WriteHeader(404)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL+"/old"), srv.Client(), nil, nil)
		if res.Valid || res.StatusCode != 404 || res.Reason != types.ReasonHTTPError {
			t.Errorf("expected broken 404 after redirect, got valid=%v status=%d reason=%q", res.Valid, res.StatusCode, res.Reason)
		}
	})

	// 401 → AUTH_BLOCKED (not broken)
	t.Run("401 auth blocked", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(401)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if res.Valid || res.Reason != types.ReasonAuthBlocked || res.StatusCode != 401 {
			t.Errorf("expected AUTH_BLOCKED, got valid=%v reason=%q status=%d", res.Valid, res.Reason, res.StatusCode)
		}
		if res.IsBroken() {
			t.Error("AUTH_BLOCKED should not count as broken")
		}
	})

	// 403 on both HEAD and GET → AUTH_BLOCKED
	t.Run("403 auth blocked", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(403)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if res.Valid || res.Reason != types.ReasonAuthBlocked {
			t.Errorf("expected AUTH_BLOCKED, got valid=%v reason=%q", res.Valid, res.Reason)
		}
	})

	// 418 (anti-bot "teapot") is reported as a broken HTTP_ERROR at validation time;
	// the resolver separately skips the AI for it (see resolver tests).
	t.Run("418 is HTTP_ERROR", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(418)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if res.Valid || res.Reason != types.ReasonHTTPError || res.StatusCode != 418 {
			t.Errorf("expected HTTP_ERROR 418, got valid=%v reason=%q status=%d", res.Valid, res.Reason, res.StatusCode)
		}
	})

	// 403 on HEAD but 200 on GET → valid (server rejects HEAD only)
	t.Run("403 HEAD fallback GET 200", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodHead {
				w.WriteHeader(403)
				return
			}
			w.WriteHeader(200)
		}))
		defer srv.Close()
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, nil)
		if !res.Valid || res.StatusCode != 200 {
			t.Errorf("expected valid via GET fallback, got valid=%v status=%d reason=%q", res.Valid, res.StatusCode, res.Reason)
		}
	})

	// Network error (unresolvable host) → NETWORK_ERROR
	t.Run("network error", func(t *testing.T) {
		client := &http.Client{Timeout: 2 * time.Second}
		res := ValidateAbsolute(absLink("https://nonexistent.invalid.host.example/"), client, nil, nil)
		if res.Valid || res.Reason != types.ReasonNetworkError {
			t.Errorf("expected NETWORK_ERROR, got valid=%v reason=%q", res.Valid, res.Reason)
		}
	})

	// Timeout
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Block until client disconnects.
			<-r.Context().Done()
		}))
		defer srv.Close()
		client := &http.Client{Timeout: 50 * time.Millisecond}
		res := ValidateAbsolute(absLink(srv.URL), client, nil, nil)
		if res.Valid || res.Reason != types.ReasonTimeout {
			t.Errorf("expected TIMEOUT, got valid=%v reason=%q", res.Valid, res.Reason)
		}
	})

	// Ignored pattern
	t.Run("ignored pattern", func(t *testing.T) {
		res := ValidateAbsolute(absLink("https://internal.example.com/page"), &http.Client{}, []string{"https://internal.*"}, nil)
		if !res.Valid || res.Reason != types.ReasonIgnored {
			t.Errorf("expected IGNORED, got valid=%v reason=%q", res.Valid, res.Reason)
		}
	})
}

func TestTokenForURL(t *testing.T) {
	tokens := map[string]string{
		"github.com":        "token-public",
		"github.tools.sap":  "token-ghe",
	}

	if got := tokenForURL("https://github.com/org/repo", tokens); got != "token-public" {
		t.Errorf("github.com: got %q, want token-public", got)
	}
	if got := tokenForURL("https://github.tools.sap/org/repo", tokens); got != "token-ghe" {
		t.Errorf("github.tools.sap: got %q, want token-ghe", got)
	}
	if got := tokenForURL("https://kubernetes.io/docs/", tokens); got != "" {
		t.Errorf("non-GitHub host: got %q, want empty", got)
	}
	if got := tokenForURL("https://github.unknown.host/x", tokens); got != "" {
		t.Errorf("unknown GHE host: got %q, want empty", got)
	}
}

