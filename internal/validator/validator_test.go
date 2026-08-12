package validator

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestValidate_Integration(t *testing.T) {
	// Set up a temp dir with one valid and one missing file.
	root := t.TempDir()
	existing := filepath.Join(root, "exists.md")
	_ = os.WriteFile(existing, []byte("# Hello\n"), 0o644)

	// Concurrent-request counter to verify cap.
	var inFlight int64
	var maxSeen int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := atomic.AddInt64(&inFlight, 1)
		for {
			old := atomic.LoadInt64(&maxSeen)
			if cur <= old || atomic.CompareAndSwapInt64(&maxSeen, old, cur) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	links := []types.Link{
		{URL: "exists.md", Type: types.LinkTypeRelative, SourceFile: existing},
		{URL: "missing.md", Type: types.LinkTypeRelative, SourceFile: existing},
		{URL: srv.URL + "/a", Type: types.LinkTypeAbsolute, SourceFile: "x.md"},
		{URL: srv.URL + "/b", Type: types.LinkTypeAbsolute, SourceFile: "x.md"},
		{URL: srv.URL + "/c", Type: types.LinkTypeAbsolute, SourceFile: "x.md"},
		{URL: srv.URL + "/d", Type: types.LinkTypeAbsolute, SourceFile: "x.md"},
		{URL: srv.URL + "/e", Type: types.LinkTypeAbsolute, SourceFile: "x.md"},
	}

	opts := ValidateOptions{Concurrency: 2, Timeout: 5 * time.Second}
	results, _ := Validate(links, opts)

	if len(results) != len(links) {
		t.Fatalf("got %d results, want %d", len(results), len(links))
	}
	if !results[0].Valid {
		t.Error("existing.md should be valid")
	}
	if results[1].Valid || results[1].Reason != types.ReasonFileNotFound {
		t.Errorf("missing.md: valid=%v reason=%q", results[1].Valid, results[1].Reason)
	}
	for i := 2; i < len(results); i++ {
		if !results[i].Valid {
			t.Errorf("result[%d] should be valid (HTTP 200), got reason=%q", i, results[i].Reason)
		}
	}
	if maxSeen > 2 {
		t.Errorf("concurrency cap violated: max in-flight was %d, want ≤2", maxSeen)
	}
}

func TestValidate_DedupesUniqueURLs(t *testing.T) {
	// Count how many times each path is actually fetched.
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	shared := srv.URL + "/shared"
	other := srv.URL + "/other"
	// The shared URL appears many times across different source files.
	links := []types.Link{
		{URL: shared, Type: types.LinkTypeAbsolute, SourceFile: "a.md"},
		{URL: shared, Type: types.LinkTypeAbsolute, SourceFile: "b.md"},
		{URL: shared, Type: types.LinkTypeAbsolute, SourceFile: "c.md"},
		{URL: other, Type: types.LinkTypeAbsolute, SourceFile: "a.md"},
	}

	var progressTotal int64
	opts := ValidateOptions{
		Concurrency: 4,
		Timeout:     5 * time.Second,
		OnProgress:  func(_, total int) { atomic.StoreInt64(&progressTotal, int64(total)) },
	}
	results, _ := Validate(links, opts)

	// Only two unique URLs → only two HTTP requests, despite four links.
	if got := atomic.LoadInt64(&hits); got != 2 {
		t.Errorf("HTTP hits = %d, want 2 (one per unique URL)", got)
	}
	// Progress total tracks unique URLs, not link count.
	if got := atomic.LoadInt64(&progressTotal); got != 2 {
		t.Errorf("progress total = %d, want 2 unique URLs", got)
	}
	// Every link gets a valid result with its own source metadata preserved.
	if len(results) != len(links) {
		t.Fatalf("got %d results, want %d", len(results), len(links))
	}
	for i, r := range results {
		if !r.Valid {
			t.Errorf("result[%d] (%s) should be valid, reason=%q", i, links[i].URL, r.Reason)
		}
		if r.Link.SourceFile != links[i].SourceFile {
			t.Errorf("result[%d] SourceFile = %q, want %q (metadata not preserved)", i, r.Link.SourceFile, links[i].SourceFile)
		}
	}
}

func TestValidate_DedupCacheHitsCountLinks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	shared := srv.URL + "/shared"
	links := []types.Link{
		{URL: shared, Type: types.LinkTypeAbsolute, SourceFile: "a.md"},
		{URL: shared, Type: types.LinkTypeAbsolute, SourceFile: "b.md"},
		{URL: shared, Type: types.LinkTypeAbsolute, SourceFile: "c.md"},
	}

	cacheFile := filepath.Join(t.TempDir(), "cache.json")
	opts := ValidateOptions{Concurrency: 2, Timeout: 5 * time.Second, CacheFile: cacheFile}

	// First run populates the cache; nothing served from cache yet.
	if _, hits := Validate(links, opts); hits != 0 {
		t.Errorf("first run cacheHits = %d, want 0", hits)
	}
	// Second run: all three links share one cached URL. cacheHits counts links,
	// not unique URLs, matching the prior accounting.
	if _, hits := Validate(links, opts); hits != 3 {
		t.Errorf("second run cacheHits = %d, want 3 (one per link sharing the cached URL)", hits)
	}
}
