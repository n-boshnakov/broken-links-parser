package resolver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchWaybackSnapshot(t *testing.T) {
	t.Run("CDX returns snapshot — title and excerpt extracted", func(t *testing.T) {
		// Snapshot content server.
		snapSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte(`<html><head><title>Kubernetes Concepts</title></head>
<body><h1>Key Concepts</h1><p>Kubernetes is an open-source container orchestration system.</p></body></html>`))
		}))
		defer snapSrv.Close()

		// CDX API server returning the snapshot URL.
		cdxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			resp := map[string]interface{}{
				"archived_snapshots": map[string]interface{}{
					"closest": map[string]interface{}{
						"available": true,
						"url":       snapSrv.URL,
						"status":    "200",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		}))
		defer cdxSrv.Close()

		// Patch the CDX URL by directly calling fetchSnapshotContent for the content part,
		// and test the CDX parsing via a helper that accepts a custom CDX URL.
		snapshotURL, title, excerpt, found := fetchWaybackWithCDX(cdxSrv.URL, snapSrv.Client())
		if !found {
			t.Fatal("expected found=true")
		}
		if snapshotURL != snapSrv.URL {
			t.Errorf("snapshotURL = %q", snapshotURL)
		}
		if title != "Kubernetes Concepts" {
			t.Errorf("title = %q, want %q", title, "Kubernetes Concepts")
		}
		if excerpt == "" {
			t.Error("expected non-empty excerpt")
		}
	})

	t.Run("CDX returns no snapshot — found=false", func(t *testing.T) {
		cdxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"archived_snapshots": map[string]interface{}{},
			})
		}))
		defer cdxSrv.Close()
		_, _, _, found := fetchWaybackWithCDX(cdxSrv.URL, cdxSrv.Client())
		if found {
			t.Error("expected found=false for no snapshot")
		}
	})

	t.Run("CDX timeout — found=false", func(t *testing.T) {
		cdxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		}))
		defer cdxSrv.Close()
		// Temporarily shorten timeout by using a client with 50ms timeout.
		_, _, _, found := fetchWaybackWithCDXClient(cdxSrv.URL, &http.Client{Timeout: 50 * time.Millisecond})
		if found {
			t.Error("expected found=false on timeout")
		}
	})

	t.Run("archived 404 — found=false", func(t *testing.T) {
		cdxSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"archived_snapshots": map[string]interface{}{
					"closest": map[string]interface{}{
						"available": true,
						"url":       "https://web.archive.org/web/20231001/http://example.com/gone",
						"status":    "404",
					},
				},
			})
		}))
		defer cdxSrv.Close()
		_, _, _, found := fetchWaybackWithCDX(cdxSrv.URL, cdxSrv.Client())
		if found {
			t.Error("expected found=false for archived 404")
		}
	})
}
