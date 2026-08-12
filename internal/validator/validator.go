package validator

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// SourceMapper is a minimal interface for looking up assembled paths.
// Avoids a direct import cycle with internal/docforge.
type SourceMapper interface {
	ContainsLocalPath(localPath string) bool
}

// ValidateOptions controls validation behaviour.
type ValidateOptions struct {
	Concurrency      int
	Timeout          time.Duration
	IgnorePatterns   []string
	ScopedIgnore     *ScopedIgnoreFile
	GitHubToken      string            // deprecated: use GitHubTokens
	GitHubTokens     map[string]string // host → token; takes precedence over GitHubToken
	RepoRoot         string
	RootRelativeBase string // base dir for /-prefixed links; empty = repo root / SourceRepo
	DocforgeStrict   bool
	SourceMap        SourceMapper
	OnProgress       func(n, total int)
	// Validation result cache options.
	CacheFile          string                    // path to JSON cache file; empty disables caching
	CacheTTL           time.Duration             // default TTL for cached results (default 24h)
	DomainTTLs         map[string]time.Duration  // per-domain TTL overrides (e.g. "pkg.go.dev": 1h)
	NoValidationCache  bool                      // when true, disables cache entirely
}

// Validate classifies each link as valid or broken.
// Relative and anchor links are checked synchronously.
// Absolute links are validated once per unique URL, concurrently up to
// opts.Concurrency workers, and the result is fanned out to every link that
// shares that URL. Returns (results, cacheHits) where cacheHits is the number
// of absolute links (not unique URLs) served from the on-disk cache.
//
// Progress reported via opts.OnProgress tracks the real network work: n and
// total count unique absolute URLs, since relative/anchor checks and duplicate
// URLs are effectively instant.
func Validate(links []types.Link, opts ValidateOptions) ([]types.ValidationResult, int) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 5
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = 24 * time.Hour
	}

	// Load validation cache.
	var cache ValidationCache
	useCache := !opts.NoValidationCache && opts.CacheFile != ""
	if useCache {
		var err error
		cache, err = LoadCache(opts.CacheFile)
		if err != nil {
			// Non-fatal — proceed without cache.
			useCache = false
		}
	}
	var cacheHits int64

	client := &http.Client{
		Timeout: opts.Timeout,
	}

	results := make([]types.ValidationResult, len(links))

	type indexedLink struct {
		idx  int
		link types.Link
	}
	var absolutes []indexedLink

	for i, l := range links {
		linkPatterns := opts.IgnorePatterns
		if opts.ScopedIgnore != nil {
			linkPatterns = opts.ScopedIgnore.PatternsFor(l.SourceFile)
			linkPatterns = append(linkPatterns, opts.IgnorePatterns...)
		}
		switch l.Type {
		case types.LinkTypeAbsolute:
			absolutes = append(absolutes, indexedLink{i, l})
		case types.LinkTypeImage:
			if strings.HasPrefix(l.URL, "http://") || strings.HasPrefix(l.URL, "https://") {
				absolutes = append(absolutes, indexedLink{i, l})
			} else {
				r := ValidateRelative(l, linkPatterns, opts.RepoRoot, opts.RootRelativeBase)
				if r.Valid && opts.DocforgeStrict && opts.SourceMap != nil && l.SourceRepo != "" {
					r.NotAssembled = !opts.SourceMap.ContainsLocalPath(resolvedPath(l, opts.RepoRoot, opts.RootRelativeBase))
				}
				results[i] = r
			}
		default:
			r := ValidateRelative(l, linkPatterns, opts.RepoRoot, opts.RootRelativeBase)
			if r.Valid && opts.DocforgeStrict && opts.SourceMap != nil && l.SourceRepo != "" {
				r.NotAssembled = !opts.SourceMap.ContainsLocalPath(resolvedPath(l, opts.RepoRoot, opts.RootRelativeBase))
			}
			results[i] = r
		}
	}

	// Group absolute links by URL so each unique URL is validated only once per
	// run. In large docs corpora the same external URLs (badges, k8s.io, etc.)
	// recur thousands of times; validating per unique URL turns hundreds of
	// thousands of potential HTTP calls into a few thousand.
	urlToIndices := make(map[string][]int)
	var uniqueURLs []string
	for _, il := range absolutes {
		if _, seen := urlToIndices[il.link.URL]; !seen {
			uniqueURLs = append(uniqueURLs, il.link.URL)
		}
		urlToIndices[il.link.URL] = append(urlToIndices[il.link.URL], il.idx)
	}
	// A representative link per URL (for ignore-pattern context and metadata).
	repLink := make(map[string]types.Link, len(uniqueURLs))
	for _, il := range absolutes {
		if _, ok := repLink[il.link.URL]; !ok {
			repLink[il.link.URL] = il.link
		}
	}

	total := len(uniqueURLs)
	var done int64
	notify := func() {
		n := atomic.AddInt64(&done, 1)
		if opts.OnProgress != nil {
			opts.OnProgress(int(n), total)
		}
	}

	// apply writes a URL's result to every link index that shares it, restoring
	// each link's own metadata.
	apply := func(url string, r types.ValidationResult) {
		for _, idx := range urlToIndices[url] {
			rr := r
			rr.Link = links[idx]
			results[idx] = rr
		}
	}

	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	var cacheMu sync.Mutex // guards all cache reads and writes
	for _, u := range uniqueURLs {
		u := u
		link := repLink[u]
		linkPatterns := opts.IgnorePatterns
		if opts.ScopedIgnore != nil {
			linkPatterns = opts.ScopedIgnore.PatternsFor(link.SourceFile)
			linkPatterns = append(linkPatterns, opts.IgnorePatterns...)
		}

		// Cache check — must hold lock because goroutines write concurrently.
		if useCache {
			cacheMu.Lock()
			cached, ok := cache.Get(u, TTLFor(u, opts.CacheTTL, opts.DomainTTLs))
			cacheMu.Unlock()
			if ok {
				apply(u, cached)
				// Count every link sharing this URL as a cache hit, matching the
				// prior per-link accounting.
				atomic.AddInt64(&cacheHits, int64(len(urlToIndices[u])))
				notify()
				continue
			}
		}

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r := ValidateAbsolute(link, client, linkPatterns, opts.GitHubTokens)
			apply(u, r)
			if useCache && cacheable(r) {
				cacheMu.Lock()
				cache.Set(u, r)
				cacheMu.Unlock()
			}
			notify()
		}()
	}
	wg.Wait()

	// Persist cache with new results.
	if useCache {
		_ = cache.Save(opts.CacheFile) // non-fatal
	}

	return results, int(atomic.LoadInt64(&cacheHits))
}

// resolvedPath computes the absolute path of a relative link target.
// rootRelativeBase, when non-empty, is the base for /-prefixed links (mirrors ValidateRelative).
func resolvedPath(l types.Link, repoRoot, rootRelativeBase string) string {
	url := l.URL
	if i := strings.Index(url, "#"); i >= 0 {
		url = url[:i]
	}
	if url == "" {
		return l.SourceFile
	}
	if strings.HasPrefix(url, "/") {
		base := rootRelativeBase
		if base == "" {
			base = l.SourceRepo
		}
		if base == "" {
			base = repoRoot
		}
		if base != "" {
			return filepath.Join(base, filepath.FromSlash(url))
		}
	}
	return filepath.Join(filepath.Dir(l.SourceFile), filepath.FromSlash(url))
}

// docforgeSourceMap is the concrete adapter used by pipeline to pass SourceMap.
// Defined here to keep the import direction clean.
type docforgeSourceMap struct {
	paths map[string]bool // set of all LocalFilePath values
}

// NewSourceMapper builds a SourceMapper from a map of assembledPath→localFilePath.
func NewSourceMapper(localPaths []string) SourceMapper {
	m := &docforgeSourceMap{paths: make(map[string]bool, len(localPaths))}
	for _, p := range localPaths {
		m.paths[filepath.Clean(p)] = true
	}
	return m
}

func (m *docforgeSourceMap) ContainsLocalPath(localPath string) bool {
	return m.paths[filepath.Clean(localPath)]
}

// fileExists is used in tests.
var fileExists = func(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

