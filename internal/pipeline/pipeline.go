package pipeline

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/docforge"
	"github.com/n-boshnakov/broken-links-parser/internal/extractor"
	"github.com/n-boshnakov/broken-links-parser/internal/resolver"
	"github.com/n-boshnakov/broken-links-parser/internal/types"
	"github.com/n-boshnakov/broken-links-parser/internal/validator"
)

// Options configures a pipeline run. It mirrors the CLI flags one-to-one.
type Options struct {
	// Extraction
	Root    string
	Dirs    []string
	Verbose bool

	// Docforge distributed documentation
	DocforgeManifest string // path to root docforge manifest YAML; empty = disabled
	DocforgeStrict   bool   // when true, flag links valid on disk but not in the manifest

	// Validation
	Validate         bool
	IgnorePatterns   []string
	IgnoreFile       string
	ScopedIgnoreFile string // path to sectioned .linkignore with per-repo patterns
	Concurrency    int
	Timeout        time.Duration
	GitHubToken    string

	// Validation result cache.
	CacheFile         string        // path to JSON cache file; default ~/.cache/broken-links-parser/validation.json
	CacheTTL          time.Duration // TTL for cached results; default 24h
	NoValidationCache bool          // disables validation caching entirely

	// Resolution
	Resolve       bool
	ReposDir      string
	CacheDir      string // directory for auto-cloned repos; empty disables auto-cloning
	NoCache       bool   // when true, disables auto-cloning
	NoFetch       bool
	AI            resolver.AIConfig
	EnableAI      bool
	EnableWayback bool

	// Output
	HTMLPath string

	// OnProgress is called after each link is validated. Passed through to ValidateOptions.
	OnProgress func(n, total int)
}

// Result holds the outputs of a completed pipeline run.
// Stages that were not run have nil/empty slices.
type Result struct {
	Links       []types.Link
	Validations []types.ValidationResult
	Resolutions []types.ResolutionResult
	SourceMap   docforge.SourceMap // nil when no docforge manifest was provided
}

// Extract runs only the extraction stage.
// When opts.DocforgeManifest is set, it also extracts links from remote-sourced
// files available in local clones under opts.ReposDir.
// Returns extracted links and the parsed SourceMap (nil if no manifest).
func Extract(opts Options) ([]types.Link, docforge.SourceMap, error) {
	links, err := extractor.Extract(opts.Root, opts.Dirs)
	if err != nil {
		return nil, nil, err
	}

	if opts.DocforgeManifest == "" {
		return links, nil, nil
	}

	sm, err := docforge.ParseManifest(opts.DocforgeManifest, opts.ReposDir, opts.CacheDir)
	if err != nil {
		return nil, nil, fmt.Errorf("parsing docforge manifest: %w", err)
	}

	// Scan each sourced file that has a local copy available.
	for _, entry := range sm {
		if entry.LocalFilePath == "" {
			continue
		}
		sourced, err := extractor.ExtractFile(entry.LocalFilePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "docforge: skipping %s: %v\n", entry.LocalFilePath, err)
			continue
		}
		for i := range sourced {
			sourced[i].SourceRepo = entry.RepoLocalClone
		}
		links = append(links, sourced...)
	}

	return links, sm, nil
}

// Validate runs only the validation stage against the provided links.
// sm is the SourceMap from Extract; pass nil when no manifest was used.
// Returns validation results and the number of absolute links served from cache.
func Validate(links []types.Link, opts Options, sm docforge.SourceMap) ([]types.ValidationResult, int, error) {
	patterns := append([]string(nil), opts.IgnorePatterns...)

	if filePatterns, err := validator.LoadIgnoreFile(filepath.Join(opts.Root, ".linkignore")); err != nil {
		return nil, 0, fmt.Errorf("reading .linkignore: %w", err)
	} else {
		patterns = append(patterns, filePatterns...)
	}

	if opts.IgnoreFile != "" {
		filePatterns, err := validator.LoadIgnoreFile(opts.IgnoreFile)
		if err != nil {
			return nil, 0, fmt.Errorf("reading ignore file: %w", err)
		}
		patterns = append(patterns, filePatterns...)
	}

	// Load sectioned ignore file if specified (supports per-repo patterns).
	var scopedIgnore *validator.ScopedIgnoreFile
	if opts.ScopedIgnoreFile != "" {
		si, err := validator.LoadScopedIgnoreFile(opts.ScopedIgnoreFile)
		if err != nil {
			return nil, 0, fmt.Errorf("reading scoped ignore file: %w", err)
		}
		scopedIgnore = &si
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	concurrency := opts.Concurrency
	if concurrency == 0 {
		concurrency = 5
	}

	vopts := validator.ValidateOptions{
		Concurrency:       concurrency,
		Timeout:           timeout,
		IgnorePatterns:    patterns,
		ScopedIgnore:      scopedIgnore,
		GitHubToken:       opts.GitHubToken,
		RepoRoot:          opts.Root,
		OnProgress:        opts.OnProgress,
		CacheFile:         opts.CacheFile,
		CacheTTL:          opts.CacheTTL,
		NoValidationCache: opts.NoValidationCache,
	}

	// Wire docforge strict mode when a manifest was provided.
	if opts.DocforgeStrict && sm != nil {
		vopts.DocforgeStrict = true
		// Build a SourceMapper from the SourceMap's local file paths.
		var localPaths []string
		for _, entry := range sm {
			if entry.LocalFilePath != "" {
				localPaths = append(localPaths, entry.LocalFilePath)
			}
		}
		vopts.SourceMap = validator.NewSourceMapper(localPaths)
	}

	results, cacheHits := validator.Validate(links, vopts)
	return results, cacheHits, nil
}

// Resolve runs only the resolution stage against the provided validation results.
func Resolve(validations []types.ValidationResult, opts Options) []types.ResolutionResult {
	return resolver.Resolve(validations, resolver.ResolveOptions{
		RepoRoot:      opts.Root,
		ReposDir:      opts.ReposDir,
		CacheDir:      opts.CacheDir,
		NoCache:       opts.NoCache,
		GitHubToken:   opts.GitHubToken,
		AI:            opts.AI,
		EnableAI:      opts.EnableAI,
		EnableWayback: opts.EnableWayback,
		NoFetch:       opts.NoFetch,
	})
}

// Run executes all configured stages in sequence and returns the combined result.
// It is silent — callers handle progress output.
func Run(opts Options) (*Result, error) {
	links, sm, err := Extract(opts)
	if err != nil {
		return nil, err
	}

	result := &Result{Links: links, SourceMap: sm}

	if opts.Validate {
		validations, _, err := Validate(links, opts, sm)
		if err != nil {
			return nil, err
		}
		result.Validations = validations

		if opts.Resolve && len(validations) > 0 {
			result.Resolutions = Resolve(validations, opts)
		}
	}

	return result, nil
}
