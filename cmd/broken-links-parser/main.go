package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"

	"github.com/n-boshnakov/broken-links-parser/internal/pipeline"
	"github.com/n-boshnakov/broken-links-parser/internal/reporter"
	"github.com/n-boshnakov/broken-links-parser/internal/resolver"
	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func main() {
	loadDotEnv(".env")
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var (
	dirs    []string
	rootDir string
)

var rootCmd = &cobra.Command{
	Use:   "broken-links-parser",
	Short: "Find and fix broken links in Markdown and HTML documentation repositories",
}

var extractCmd = &cobra.Command{
	Use:   "extract",
	Short: "Extract all links from a repository and print a summary",
	RunE: func(cmd *cobra.Command, _ []string) error {
		opts := buildOptions(cmd)
		runStart := time.Now()

		// Stage 1: Extract
		links, sourceMap, err := pipeline.Extract(opts)
		if err != nil {
			return err
		}
		fmt.Printf("Found %d links in %s\n", len(links), opts.Root)
		if opts.Verbose {
			for _, l := range links {
				rel, _ := strings.CutPrefix(l.SourceFile, opts.Root+"/")
				fmt.Printf("  [%s] %s  (%s)\n", l.Type, l.URL, rel)
			}
		}

		result := &pipeline.Result{Links: links}

		// Stage 2: Validate
		if opts.Validate {
			if opts.GitHubToken != "" {
				fmt.Println("GitHub token detected — authenticated requests will be used for github.com URLs.")
			}
			total := len(links)
			fmt.Printf("Validating %d links (concurrency=%d, timeout=%s)…\n", total, opts.Concurrency, opts.Timeout)

			valStart := time.Now()
			var progressMu sync.Mutex
			opts.OnProgress = func(n, tot int) {
				if n%50 != 0 && n != tot {
					return
				}
				elapsed := time.Since(valStart)
				var eta string
				if n > 0 && n < tot {
					remaining := time.Duration(float64(elapsed) / float64(n) * float64(tot-n))
					eta = fmt.Sprintf(", ETA %s", remaining.Round(time.Second))
				}
				progressMu.Lock()
				fmt.Printf("\r  %d/%d validated (%.0f%%)%s   ", n, tot, float64(n)/float64(tot)*100, eta)
				progressMu.Unlock()
			}

			validations, err := pipeline.Validate(links, opts, sourceMap)
			fmt.Println() // end progress line
			if err != nil {
				return err
			}
			result.Validations = validations
			broken := 0
			for _, r := range validations {
				if !r.Valid && r.Reason != types.ReasonIgnored {
					broken++
				}
			}
			fmt.Printf("Validation complete: %d broken, %d valid (%s)\n",
				broken, len(links)-broken, time.Since(valStart).Round(time.Millisecond))

			// Stage 3: Resolve
			if opts.Resolve && len(validations) > 0 {
				if opts.EnableAI && opts.AI.APIKey == "" {
					fmt.Fprintln(os.Stderr, "Warning: --ai set but AI_API_KEY not found in environment or .env")
				} else if opts.EnableAI {
					fmt.Printf("AI resolution enabled (model: %s, openai-compat: %v)\n", opts.AI.Model, opts.AI.BaseURL != "")
				}
				if opts.EnableWayback {
					fmt.Println("Wayback Machine enrichment enabled.")
				}
				resolvStart := time.Now()
				fmt.Println("Resolving broken links…")
				resolutions := pipeline.Resolve(validations, opts)
				result.Resolutions = resolutions
				resolved := 0
				for _, r := range resolutions {
					if r.FixedURL != "" {
						resolved++
					}
				}
				fmt.Printf("Resolution complete: %d fixed, %d unresolved (%s)\n",
					resolved, len(resolutions)-resolved, time.Since(resolvStart).Round(time.Millisecond))
			}
		}

		// Stage 4: Report
		if opts.HTMLPath != "" {
			if err := reporter.WriteHTML(opts.HTMLPath, opts.Root, result); err != nil {
				return fmt.Errorf("writing HTML report: %w", err)
			}
			fmt.Printf("HTML report written to %s\n", opts.HTMLPath)
		}

		fmt.Printf("Total run time: %s\n", time.Since(runStart).Round(time.Millisecond))
		return nil
	},
}

func buildOptions(cmd *cobra.Command) pipeline.Options {
	verbose, _ := cmd.Flags().GetBool("verbose")
	validate, _ := cmd.Flags().GetBool("validate")
	ignorePatterns, _ := cmd.Flags().GetStringArray("ignore-pattern")
	ignoreFile, _ := cmd.Flags().GetString("ignore-file")
	scopedIgnoreFile, _ := cmd.Flags().GetString("scoped-ignore-file")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	resolve, _ := cmd.Flags().GetBool("resolve")
	reposDir, _ := cmd.Flags().GetString("repos-dir")
	noFetch, _ := cmd.Flags().GetBool("no-fetch")
	cacheDir, _ := cmd.Flags().GetString("cache-dir")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	// Expand ~ in cacheDir.
	if len(cacheDir) >= 2 && cacheDir[:2] == "~/" {
		if home, err := os.UserHomeDir(); err == nil {
			cacheDir = home + cacheDir[1:]
		}
	}
	enableAI, _ := cmd.Flags().GetBool("ai")
	enableWayback, _ := cmd.Flags().GetBool("wayback")
	docforgeManifest, _ := cmd.Flags().GetString("docforge-manifest")
	docforgeStrict, _ := cmd.Flags().GetBool("docforge-strict")
	htmlPath, _ := cmd.Flags().GetString("html")

	return pipeline.Options{
		Root:           rootDir,
		Dirs:           dirs,
		Verbose:        verbose,
		Validate:       validate,
		IgnorePatterns: ignorePatterns,
		IgnoreFile:       ignoreFile,
		ScopedIgnoreFile: scopedIgnoreFile,
		Concurrency:    concurrency,
		Timeout:        timeout,
		GitHubToken:    os.Getenv("GITHUB_TOKEN"),
		Resolve:        resolve,
		ReposDir:       reposDir,
		CacheDir:       cacheDir,
		NoCache:        noCache,
		NoFetch:        noFetch,
		AI:             resolver.AIConfigFromEnv(),
		EnableAI:       enableAI,
		EnableWayback:    enableWayback,
		DocforgeManifest: docforgeManifest,
		DocforgeStrict:   docforgeStrict,
		HTMLPath:         htmlPath,
	}
}

func init() {
	extractCmd.Flags().StringVar(&rootDir, "root", ".", "Repository root directory")
	extractCmd.Flags().StringSliceVar(&dirs, "dirs", nil, "Restrict scan to comma-separated subdirectories")
	extractCmd.Flags().Bool("verbose", false, "Print each link")
	extractCmd.Flags().String("html", "", "Write an HTML report to this file path (default: reports/report.html)")
	extractCmd.Flags().Bool("validate", false, "Validate each link after extraction")
	extractCmd.Flags().StringArray("ignore-pattern", nil, "Skip links matching this glob pattern (repeatable)")
	extractCmd.Flags().String("ignore-file", "", "Path to a simple ignore patterns file (one per line, # for comments)")
	extractCmd.Flags().String("scoped-ignore-file", "", "Path to a sectioned ignore file with per-repo patterns (see docs/validation.md)")
	extractCmd.Flags().Int("concurrency", 5, "Max concurrent HTTP requests during validation")
	extractCmd.Flags().Duration("timeout", 15*time.Second, "Per-link HTTP timeout")
	extractCmd.Flags().Bool("resolve", false, "Attempt to resolve broken links after validation")
	extractCmd.Flags().String("repos-dir", "", "Directory containing local repo clones for faster resolution (e.g. ~/Documents/GitHub)")
	extractCmd.Flags().Bool("no-fetch", false, "Skip git fetch when using local clones for resolution")
	extractCmd.Flags().String("cache-dir", "~/.cache/broken-links-parser/clones", "Directory for auto-cloned repos; set empty to disable auto-cloning")
	extractCmd.Flags().Bool("no-cache", false, "Disable auto-cloning; fall back to GitHub API when no local clone is found")
	extractCmd.Flags().Bool("ai", false, "Use Claude AI as last-resort resolver for external links (requires AI_API_KEY)")
	extractCmd.Flags().Bool("apply-ai", false, "Allow AI-suggested fixes to be applied by the repair stage")
	extractCmd.Flags().Bool("wayback", false, "Enrich AI resolution with Wayback Machine context and use as fallback (requires --ai)")
	extractCmd.Flags().String("docforge-manifest", "", "Path to root docforge manifest YAML; enables extraction from remote-sourced files via local clones")
	extractCmd.Flags().Bool("docforge-strict", false, "Flag valid links whose target file is not included in the docforge manifest (requires --docforge-manifest)")
	rootCmd.AddCommand(extractCmd)
}

// loadDotEnv reads a .env file and sets any unset environment variables from it.
// Lines starting with # and blank lines are ignored. Already-set env vars are not overridden.
func loadDotEnv(path string) {
	f, err := os.Open(path)
	if err != nil {
		return // missing .env is fine
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if key == "" || os.Getenv(key) != "" {
			continue // don't override existing env vars
		}
		os.Setenv(key, val)
	}
}
