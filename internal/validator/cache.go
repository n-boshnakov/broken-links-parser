package validator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// CacheEntry stores a validation result alongside when it was checked.
type CacheEntry struct {
	Result    types.ValidationResult `json:"result"`
	CheckedAt time.Time              `json:"checked_at"`
}

// ValidationCache maps URL → CacheEntry. Load with LoadCache, save with Save.
type ValidationCache map[string]CacheEntry

// LoadCache reads a JSON cache file. Returns an empty cache (not an error) if the file
// does not exist, allowing first-run use without pre-creating the file.
func LoadCache(path string) (ValidationCache, error) {
	c := make(ValidationCache)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(data, &c); err != nil {
		// Corrupted cache — start fresh rather than failing the run.
		return make(ValidationCache), nil
	}
	return c, nil
}

// Save writes the cache to path atomically (temp file + rename).
// Creates parent directories on first use.
func (c ValidationCache) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Get returns a cached result if the URL has a valid (non-expired) entry.
func (c ValidationCache) Get(url string, ttl time.Duration) (types.ValidationResult, bool) {
	entry, ok := c[url]
	if !ok {
		return types.ValidationResult{}, false
	}
	if time.Since(entry.CheckedAt) > ttl {
		return types.ValidationResult{}, false
	}
	return entry.Result, true
}

// Set stores a validation result for url with the current timestamp.
func (c ValidationCache) Set(url string, result types.ValidationResult) {
	// Store only the URL and type — SourceFile/offsets are link-instance-specific.
	stripped := result
	stripped.Link = types.Link{
		URL:  result.Link.URL,
		Type: result.Link.Type,
	}
	c[url] = CacheEntry{Result: stripped, CheckedAt: time.Now()}
}
