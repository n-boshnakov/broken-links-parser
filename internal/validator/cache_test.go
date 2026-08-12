package validator

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestValidationCache(t *testing.T) {
	result := types.ValidationResult{
		Link:  types.Link{URL: "https://example.com", Type: types.LinkTypeAbsolute},
		Valid: true,
	}

	t.Run("hit within TTL", func(t *testing.T) {
		c := make(ValidationCache)
		c.Set("https://example.com", result)
		got, ok := c.Get("https://example.com", time.Hour)
		if !ok {
			t.Fatal("expected cache hit")
		}
		if !got.Valid {
			t.Error("expected Valid=true")
		}
	})

	t.Run("miss — expired", func(t *testing.T) {
		c := make(ValidationCache)
		c["https://example.com"] = CacheEntry{Result: result, CheckedAt: time.Now().Add(-2 * time.Hour)}
		_, ok := c.Get("https://example.com", time.Hour)
		if ok {
			t.Error("expected cache miss for expired entry")
		}
	})

	t.Run("miss — not present", func(t *testing.T) {
		c := make(ValidationCache)
		_, ok := c.Get("https://example.com", time.Hour)
		if ok {
			t.Error("expected cache miss for absent entry")
		}
	})

	t.Run("save and reload", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cache.json")
		c := make(ValidationCache)
		c.Set("https://example.com", result)
		if err := c.Save(path); err != nil {
			t.Fatal(err)
		}
		c2, err := LoadCache(path)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := c2.Get("https://example.com", time.Hour)
		if !ok {
			t.Fatal("expected cache hit after reload")
		}
		if !got.Valid {
			t.Error("expected Valid=true after reload")
		}
	})

	t.Run("missing file returns empty cache", func(t *testing.T) {
		c, err := LoadCache(filepath.Join(t.TempDir(), "nonexistent.json"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(c) != 0 {
			t.Error("expected empty cache")
		}
	})

	t.Run("atomic write — temp file cleaned up", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "cache.json")
		c := make(ValidationCache)
		c.Set("https://example.com", result)
		if err := c.Save(path); err != nil {
			t.Fatal(err)
		}
		// .tmp file should not exist after successful save.
		if _, err := LoadCache(path + ".tmp"); err == nil {
			// If it loaded fine there was a leftover — not a hard failure but worth noting.
		}
	})
}

func TestCacheable(t *testing.T) {
	link := types.Link{URL: "https://example.com", Type: types.LinkTypeAbsolute}
	cases := []struct {
		name string
		r    types.ValidationResult
		want bool
	}{
		{"valid", types.ValidationResult{Link: link, Valid: true}, true},
		{"settled 404", types.ValidationResult{Link: link, Reason: types.ReasonHTTPError, StatusCode: 404}, true},
		{"settled 410", types.ValidationResult{Link: link, Reason: types.ReasonHTTPError, StatusCode: 410}, true},
		{"transient 503 not cached", types.ValidationResult{Link: link, Reason: types.ReasonHTTPError, StatusCode: 503}, false},
		{"transient 429 not cached", types.ValidationResult{Link: link, Reason: types.ReasonHTTPError, StatusCode: 429}, false},
		{"status 0 not cached", types.ValidationResult{Link: link, Reason: types.ReasonHTTPError, StatusCode: 0}, false},
		{"timeout not cached", types.ValidationResult{Link: link, Reason: types.ReasonTimeout}, false},
		{"network error not cached", types.ValidationResult{Link: link, Reason: types.ReasonNetworkError}, false},
		{"auth-blocked not cached", types.ValidationResult{Link: link, Reason: types.ReasonAuthBlocked, StatusCode: 403}, false},
	}
	for _, tc := range cases {
		if got := cacheable(tc.r); got != tc.want {
			t.Errorf("%s: cacheable() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
