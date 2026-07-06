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
	Concurrency    int
	Timeout        time.Duration
	IgnorePatterns []string      // flat global patterns (--ignore-pattern / simple .linkignore)
	ScopedIgnore   *ScopedIgnoreFile // sectioned ignore file; nil = disabled
	GitHubToken    string
	RepoRoot       string
	DocforgeStrict bool
	SourceMap      SourceMapper
	OnProgress     func(n, total int)
}

// Validate classifies each link as valid or broken.
// Relative and anchor links are checked synchronously.
// Absolute links are checked concurrently up to opts.Concurrency workers.
func Validate(links []types.Link, opts ValidateOptions) []types.ValidationResult {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 5
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}

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
		// Compute patterns for this specific link (global + repo-specific).
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
	for _, il := range absolutes {
		il := il
		// Compute patterns for absolute links too.
		linkPatterns := opts.IgnorePatterns
		if opts.ScopedIgnore != nil {
			linkPatterns = opts.ScopedIgnore.PatternsFor(il.link.SourceFile)
			linkPatterns = append(linkPatterns, opts.IgnorePatterns...)
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[il.idx] = ValidateAbsolute(il.link, client, linkPatterns, opts.GitHubToken)
			notify()
		}()
	}
	wg.Wait()

	return results
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

