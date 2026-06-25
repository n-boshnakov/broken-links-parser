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
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, "")
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
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, "")
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
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, "")
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
		res := ValidateAbsolute(absLink(srv.URL), srv.Client(), nil, "")
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
		res := ValidateAbsolute(absLink(srv.URL+"/old"), srv.Client(), nil, "")
		if !res.Valid {
			t.Errorf("expected valid after redirect, got valid=%v status=%d", res.Valid, res.StatusCode)
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
		res := ValidateAbsolute(absLink(srv.URL), client, nil, "")
		if res.Valid || res.Reason != types.ReasonTimeout {
			t.Errorf("expected TIMEOUT, got valid=%v reason=%q", res.Valid, res.Reason)
		}
	})

	// Ignored pattern
	t.Run("ignored pattern", func(t *testing.T) {
		res := ValidateAbsolute(absLink("https://internal.example.com/page"), &http.Client{}, []string{"https://internal.*"}, "")
		if !res.Valid || res.Reason != types.ReasonIgnored {
			t.Errorf("expected IGNORED, got valid=%v reason=%q", res.Valid, res.Reason)
		}
	})
}
