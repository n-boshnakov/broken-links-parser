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
	GitHubToken      string
	RepoRoot         string
	DocforgeStrict   bool
	SourceMap        SourceMapper
	OnProgress       func(n, total int)
	// Validation result cache options.
	CacheFile          string        // path to JSON cache file; empty disables caching
	CacheTTL           time.Duration // how long cached results remain valid (default 24h)
	NoValidationCache  bool          // when true, disables cache entirely
}

// Validate classifies each link as valid or broken.
// Relative and anchor links are checked synchronously.
// Absolute links are checked concurrently up to opts.Concurrency workers.
// Returns (results, cacheHits) where cacheHits is the number of absolute links served from cache.
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
	total := len(links)
	var done int64

	notify := func() {
		if opts.OnProgress != nil {
			opts.OnProgress(int(atomic.AddInt64(&done, 1)), total)
		} else {
			atomic.AddInt64(&done, 1)
		}
	}

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
				r := ValidateRelative(l, linkPatterns, opts.RepoRoot)
				if r.Valid && opts.DocforgeStrict && opts.SourceMap != nil && l.SourceRepo != "" {
					r.NotAssembled = !opts.SourceMap.ContainsLocalPath(resolvedPath(l, opts.RepoRoot))
				}
				results[i] = r
				notify()
			}
		default:
			r := ValidateRelative(l, linkPatterns, opts.RepoRoot)
			if r.Valid && opts.DocforgeStrict && opts.SourceMap != nil && l.SourceRepo != "" {
				r.NotAssembled = !opts.SourceMap.ContainsLocalPath(resolvedPath(l, opts.RepoRoot))
			}
			results[i] = r
			notify()
		}
	}

	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	var cacheMu sync.Mutex // guards all cache reads and writes
	for _, il := range absolutes {
		il := il
		linkPatterns := opts.IgnorePatterns
		if opts.ScopedIgnore != nil {
			linkPatterns = opts.ScopedIgnore.PatternsFor(il.link.SourceFile)
			linkPatterns = append(linkPatterns, opts.IgnorePatterns...)
		}

		// Cache check — must hold lock because goroutines write concurrently.
		if useCache {
			cacheMu.Lock()
			cached, ok := cache.Get(il.link.URL, opts.CacheTTL)
			cacheMu.Unlock()
			if ok {
				cached.Link = il.link
				results[il.idx] = cached
				atomic.AddInt64(&cacheHits, 1)
				notify()
				continue
			}
		}

		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			r := ValidateAbsolute(il.link, client, linkPatterns, opts.GitHubToken)
			results[il.idx] = r
			if useCache {
				cacheMu.Lock()
				cache.Set(il.link.URL, r)
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
func resolvedPath(l types.Link, repoRoot string) string {
	url := l.URL
	if i := strings.Index(url, "#"); i >= 0 {
		url = url[:i]
	}
	if url == "" {
		return l.SourceFile
	}
	if strings.HasPrefix(url, "/") {
		base := l.SourceRepo
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

