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
	results := Validate(links, opts)

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
