package validator

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// CacheEntry stores a validation result alongside when it was checked.
type CacheEntry struct {
	Result    types.ValidationResult `json:"result"`
	CheckedAt time.Time              `json:"checked_at"`
}

// ValidationCache maps URL → CacheEntry.
type ValidationCache map[string]CacheEntry

// LoadCache reads a JSON cache file. Returns empty cache if file doesn't exist.
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
		return make(ValidationCache), nil
	}
	return c, nil
}

// Save writes the cache to path atomically.
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

// TTLFor returns the effective TTL for a URL, checking per-domain overrides first.
func TTLFor(rawURL string, defaultTTL time.Duration, domainTTLs map[string]time.Duration) time.Duration {
	if len(domainTTLs) == 0 {
		return defaultTTL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return defaultTTL
	}
	host := strings.TrimPrefix(u.Hostname(), "www.")
	if ttl, ok := domainTTLs[host]; ok {
		return ttl
	}
	// Check suffix matches (e.g. "pkg.go.dev" matches "pkg.go.dev")
	for domain, ttl := range domainTTLs {
		if strings.HasSuffix(host, domain) {
			return ttl
		}
	}
	return defaultTTL
}

// Get returns a cached result if the URL has a valid (non-expired) entry.
func (c ValidationCache) Get(rawURL string, ttl time.Duration) (types.ValidationResult, bool) {
	entry, ok := c[rawURL]
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
	stripped := result
	stripped.Link = types.Link{
		URL:  result.Link.URL,
		Type: result.Link.Type,
	}
	c[url] = CacheEntry{Result: stripped, CheckedAt: time.Now()}
}

// cacheable reports whether a validation result is stable enough to persist.
// Transient outcomes (timeouts, network errors, auth/bot blocks, and retryable
// HTTP statuses such as 429/5xx) are provisional — caching them would let a
// momentary blip masquerade as a broken link for the full TTL, so they are
// re-checked on every run instead.
func cacheable(r types.ValidationResult) bool {
	if r.Valid {
		return true
	}
	switch r.Reason {
	case types.ReasonTimeout, types.ReasonNetworkError, types.ReasonAuthBlocked:
		return false
	case types.ReasonHTTPError:
		// A settled 4xx (e.g. 404/410) is stable; a transient status or an
		// unknown status (0, from a transport error) is not.
		if r.StatusCode == 0 || retryableStatus(r.StatusCode) {
			return false
		}
		return true
	}
	// Anything else (e.g. FILE_NOT_FOUND, ANCHOR_NOT_FOUND) is not an absolute-URL
	// result and never reaches the cache path anyway; be conservative and skip it.
	return false
}
