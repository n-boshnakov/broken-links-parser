package pipeline

import (
	"fmt"
	"path/filepath"
	"time"

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

	// Validation
	Validate       bool
	IgnorePatterns []string
	IgnoreFile     string
	Concurrency    int
	Timeout        time.Duration
	GitHubToken    string

	// Resolution
	Resolve       bool
	ReposDir      string
	NoFetch       bool
	AI            resolver.AIConfig
	EnableAI      bool
	EnableWayback bool

	// Output
	HTMLPath string
}

// Result holds the outputs of a completed pipeline run.
// Stages that were not run have nil/empty slices.
type Result struct {
	Links       []types.Link
	Validations []types.ValidationResult
	Resolutions []types.ResolutionResult
}

// Extract runs only the extraction stage.
func Extract(opts Options) ([]types.Link, error) {
	return extractor.Extract(opts.Root, opts.Dirs)
}

// Validate runs only the validation stage against the provided links.
// It loads ignore patterns from the repo root's .linkignore and the IgnoreFile option.
func Validate(links []types.Link, opts Options) ([]types.ValidationResult, error) {
	patterns := append([]string(nil), opts.IgnorePatterns...)

	// Auto-load .linkignore from the scanned repo root.
	if filePatterns, err := validator.LoadIgnoreFile(filepath.Join(opts.Root, ".linkignore")); err != nil {
		return nil, fmt.Errorf("reading .linkignore: %w", err)
	} else {
		patterns = append(patterns, filePatterns...)
	}

	// Apply additional ignore file if provided.
	if opts.IgnoreFile != "" {
		filePatterns, err := validator.LoadIgnoreFile(opts.IgnoreFile)
		if err != nil {
			return nil, fmt.Errorf("reading ignore file: %w", err)
		}
		patterns = append(patterns, filePatterns...)
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	concurrency := opts.Concurrency
	if concurrency == 0 {
		concurrency = 5
	}

	return validator.Validate(links, validator.ValidateOptions{
		Concurrency:    concurrency,
		Timeout:        timeout,
		IgnorePatterns: patterns,
		GitHubToken:    opts.GitHubToken,
	}), nil
}

// Resolve runs only the resolution stage against the provided validation results.
func Resolve(validations []types.ValidationResult, opts Options) []types.ResolutionResult {
	return resolver.Resolve(validations, resolver.ResolveOptions{
		RepoRoot:      opts.Root,
		ReposDir:      opts.ReposDir,
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
	links, err := Extract(opts)
	if err != nil {
		return nil, err
	}

	result := &Result{Links: links}

	if opts.Validate {
		validations, err := Validate(links, opts)
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
