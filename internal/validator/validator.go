package validator

import (
	"net/http"
	"sync"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// ValidateOptions controls validation behaviour.
type ValidateOptions struct {
	Concurrency    int
	Timeout        time.Duration
	IgnorePatterns []string
	GitHubToken    string
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

	// Separate relative from absolute to process them differently.
	type indexedLink struct {
		idx  int
		link types.Link
	}
	var absolutes []indexedLink

	for i, l := range links {
		switch l.Type {
		case types.LinkTypeAbsolute:
			absolutes = append(absolutes, indexedLink{i, l})
		default:
			results[i] = ValidateRelative(l, opts.IgnorePatterns)
		}
	}

	// Process absolute links concurrently with a semaphore.
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	for _, il := range absolutes {
		il := il
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[il.idx] = ValidateAbsolute(il.link, client, opts.IgnorePatterns, opts.GitHubToken)
		}()
	}
	wg.Wait()

	return results
}
