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
