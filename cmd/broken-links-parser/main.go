package main

import (
	"bufio"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/n-boshnakov/broken-links-parser/internal/extractor"
	"github.com/n-boshnakov/broken-links-parser/internal/resolver"
	"github.com/n-boshnakov/broken-links-parser/internal/types"
	"github.com/n-boshnakov/broken-links-parser/internal/validator"
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
		links, err := extractor.Extract(rootDir, dirs)
		if err != nil {
			return err
		}
		fmt.Printf("Found %d links in %s\n", len(links), rootDir)

		if verbose, _ := cmd.Flags().GetBool("verbose"); verbose {
			for _, l := range links {
				rel, _ := strings.CutPrefix(l.SourceFile, rootDir+"/")
				fmt.Printf("  [%s] %s  (%s)\n", l.Type, l.URL, rel)
			}
		}

		var results []types.ValidationResult
		var resolutions []types.ResolutionResult
		if doValidate, _ := cmd.Flags().GetBool("validate"); doValidate {
			patterns, _ := cmd.Flags().GetStringArray("ignore-pattern")

			// Auto-load .linkignore from the scanned repo root, then apply --ignore-file on top.
			if filePatterns, err := validator.LoadIgnoreFile(filepath.Join(rootDir, ".linkignore")); err != nil {
				return fmt.Errorf("reading .linkignore: %w", err)
			} else {
				patterns = append(patterns, filePatterns...)
			}
			if ignoreFile, _ := cmd.Flags().GetString("ignore-file"); ignoreFile != "" {
				filePatterns, err := validator.LoadIgnoreFile(ignoreFile)
				if err != nil {
					return fmt.Errorf("reading ignore file: %w", err)
				}
				patterns = append(patterns, filePatterns...)
			}
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			timeout, _ := cmd.Flags().GetDuration("timeout")
			githubToken := os.Getenv("GITHUB_TOKEN")
			if githubToken != "" {
				fmt.Println("GitHub token detected — authenticated requests will be used for github.com URLs.")
			}
			fmt.Printf("Validating %d links (concurrency=%d, timeout=%s)…\n", len(links), concurrency, timeout)
			results = validator.Validate(links, validator.ValidateOptions{
				Concurrency:    concurrency,
				Timeout:        timeout,
				IgnorePatterns: patterns,
				GitHubToken:    githubToken,
			})
			broken := 0
			for _, r := range results {
				if !r.Valid && r.Reason != types.ReasonIgnored {
					broken++
				}
			}
			fmt.Printf("Validation complete: %d broken, %d valid\n", broken, len(links)-broken)
		}

		if doResolve, _ := cmd.Flags().GetBool("resolve"); doResolve && len(results) > 0 {
			reposDir, _ := cmd.Flags().GetString("repos-dir")
			noFetch, _ := cmd.Flags().GetBool("no-fetch")
			enableAI, _ := cmd.Flags().GetBool("ai")
			fmt.Println("Resolving broken links…")
			resolutions = resolver.Resolve(results, resolver.ResolveOptions{
				RepoRoot:    rootDir,
				ReposDir:    reposDir,
				GitHubToken: os.Getenv("GITHUB_TOKEN"),
				AIKey:       os.Getenv("ANTHROPIC_API_KEY"),
				EnableAI:    enableAI,
				NoFetch:     noFetch,
			})
			resolved := 0
			for _, r := range resolutions {
				if r.FixedURL != "" {
					resolved++
				}
			}
			fmt.Printf("Resolution complete: %d fixed, %d unresolved\n", resolved, len(resolutions)-resolved)
		}

		if out, _ := cmd.Flags().GetString("html"); out != "" {
			if err := writeHTMLReport(out, rootDir, links, results, resolutions); err != nil {
				return fmt.Errorf("writing HTML report: %w", err)
			}
			fmt.Printf("HTML report written to %s\n", out)
		}
		return nil
	},
}

func init() {
	extractCmd.Flags().StringVar(&rootDir, "root", ".", "Repository root directory")
	extractCmd.Flags().StringSliceVar(&dirs, "dirs", nil, "Restrict scan to comma-separated subdirectories")
	extractCmd.Flags().Bool("verbose", false, "Print each link")
	extractCmd.Flags().String("html", "", "Write an HTML report to this file path (default: reports/report.html)")
	extractCmd.Flags().Bool("validate", false, "Validate each link after extraction")
	extractCmd.Flags().StringArray("ignore-pattern", nil, "Skip links matching this glob pattern (repeatable)")
	extractCmd.Flags().String("ignore-file", "", "Path to a file containing ignore patterns (one per line, # for comments)")
	extractCmd.Flags().Int("concurrency", 5, "Max concurrent HTTP requests during validation")
	extractCmd.Flags().Duration("timeout", 15*time.Second, "Per-link HTTP timeout")
	extractCmd.Flags().Bool("resolve", false, "Attempt to resolve broken links after validation")
	extractCmd.Flags().String("repos-dir", "", "Directory containing local repo clones for faster resolution (e.g. ~/Documents/GitHub)")
	extractCmd.Flags().Bool("no-fetch", false, "Skip git fetch when using local clones for resolution")
	extractCmd.Flags().Bool("ai", false, "Use Claude AI as last-resort resolver for external links (requires ANTHROPIC_API_KEY)")
	extractCmd.Flags().Bool("apply-ai", false, "Allow AI-suggested fixes to be applied by the repair stage")
	rootCmd.AddCommand(extractCmd)
}

var reportTmpl = template.Must(template.New("report").Funcs(template.FuncMap{
	"isAbsolute": func(r reportRow) bool { return r.Type == types.LinkTypeAbsolute },
	"statusClass": func(r reportRow) string {
		if r.Result == nil {
			return ""
		}
		if r.Result.Reason == types.ReasonIgnored {
			return "ignored"
		}
		if r.Result.Valid {
			return "valid"
		}
		return "broken"
	},
	"statusLabel": func(r reportRow) string {
		if r.Result == nil {
			return ""
		}
		if r.Result.Reason == types.ReasonIgnored {
			return "Ignored"
		}
		if r.Result.Valid {
			return "Valid"
		}
		return "Broken"
	},
	"reasonLabel": func(r reportRow) string {
		if r.Result == nil || r.Result.Valid {
			return ""
		}
		if r.Result.Reason == types.ReasonHTTPError && r.Result.StatusCode != 0 {
			return fmt.Sprintf("%s %d", r.Result.Reason, r.Result.StatusCode)
		}
		return r.Result.Reason
	},
	"hasValidation": func(rows []reportRow) bool {
		return len(rows) > 0 && rows[0].Result != nil
	},
	"hasResolution": func(rows []reportRow) bool {
		for _, r := range rows {
			if r.Resolution != nil {
				return true
			}
		}
		return false
	},
	"fixedURL": func(r reportRow) string {
		if r.Resolution == nil {
			return ""
		}
		return r.Resolution.FixedURL
	},
	"fixedLabel": func(r reportRow) string {
		if r.Resolution == nil || r.Resolution.FixedURL == "" {
			return ""
		}
		if r.Resolution.Deleted {
			return "Deleted in commit: " + r.Resolution.FixedURL
		}
		return r.Resolution.FixedURL
	},
	"unresolvedReason": func(r reportRow) string {
		if r.Resolution == nil || r.Resolution.FixedURL != "" {
			return ""
		}
		switch r.Resolution.UnresolvedReason {
		case types.UnresolvedAPIBlocked:
			return "API blocked (token policy)"
		case types.UnresolvedAPIRateLimit:
			return "API rate limited"
		case types.UnresolvedRepoNotFound:
			return "Repo not found or private"
		case types.UnresolvedAmbiguous:
			return "Ambiguous (multiple matches)"
		case types.UnresolvedExternalNoAI:
			return "External link (enable --ai)"
		case types.UnresolvedAIFailed:
			return "AI returned no suggestion"
		case types.UnresolvedNoHistory:
			return "No history found"
		default:
			return ""
		}
	},
	"strategyLabel": func(r reportRow) string {
		if r.Resolution == nil || r.Resolution.Strategy == "" {
			return ""
		}
		if r.Resolution.Strategy == types.StrategyAI {
			return "AI (low confidence)"
		}
		return r.Resolution.Strategy
	},
	"strategyClass": func(r reportRow) string {
		if r.Resolution == nil {
			return ""
		}
		if r.Resolution.Strategy == types.StrategyAI {
			return "strategy-ai"
		}
		return "strategy-normal"
	},
}).Parse(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Link Report — {{.Root}}</title>
<style>
  body { font-family: sans-serif; margin: 2rem; color: #222; }
  h1 { font-size: 1.4rem; }
  p.meta { color: #666; font-size: .9rem; }
  input[type=search] { padding: .4rem .6rem; font-size: 1rem; width: 24rem; margin-bottom: 1rem; }
  table { border-collapse: collapse; width: 100%; font-size: .9rem; }
  th { background: #f0f0f0; text-align: left; padding: .5rem .75rem; border-bottom: 2px solid #ccc; cursor: pointer; user-select: none; }
  td { padding: .4rem .75rem; border-bottom: 1px solid #e0e0e0; vertical-align: top; word-break: break-all; }
  tr:hover td { background: #fafafa; }
  tr.row-broken td { background: #fff1f2; }
  .badge { display:inline-block; padding:.15rem .4rem; border-radius:3px; font-size:.75rem; font-weight:600; }
  .relative { background:#dbeafe; color:#1e40af; }
  .absolute { background:#dcfce7; color:#166534; }
  .anchor   { background:#fef9c3; color:#854d0e; }
  .image    { background:#fce7f3; color:#9d174d; }
  .valid    { background:#dcfce7; color:#166534; }
  .broken   { background:#fee2e2; color:#991b1b; }
  .ignored  { background:#f3f4f6; color:#6b7280; }
  .strategy-normal { background:#dbeafe; color:#1e40af; }
  .strategy-ai     { background:#fef9c3; color:#854d0e; }
  .unresolved-reason { color:#6b7280; font-size:.8rem; font-style:italic; }
  a { color: #2563eb; }
</style>
</head>
<body>
<h1>Link Report</h1>
<p class="meta">Root: <code>{{.Root}}</code> &nbsp;·&nbsp; {{len .Links}} links found{{if .BrokenCount}} &nbsp;·&nbsp; <strong style="color:#991b1b">{{.BrokenCount}} broken</strong>{{end}}</p>
<input type="search" id="q" placeholder="Filter by URL or source file…" oninput="filter()">
<table id="tbl">
  <thead>
    <tr>
      <th onclick="sort(0)">Type</th>
      <th onclick="sort(1)">URL</th>
      <th onclick="sort(2)">Source file</th>
      {{if hasValidation .Links}}<th onclick="sort(3)">Status</th>{{end}}
      {{if hasResolution .Links}}<th onclick="sort(4)">Fixed Link</th><th onclick="sort(5)">Strategy</th>{{end}}
    </tr>
  </thead>
  <tbody>
  {{range .Links}}
    <tr{{if eq (statusClass .) "broken"}} class="row-broken"{{end}}>
      <td><span class="badge {{.Type}}">{{.Type}}</span></td>
      <td>{{if isAbsolute .}}<a href="{{.URL}}" target="_blank" rel="noopener">{{.URL}}</a>{{else}}{{.URL}}{{end}}</td>
      <td>{{.Rel}}</td>
      {{if .Result}}<td><span class="badge {{statusClass .}}">{{statusLabel .}}</span>{{if reasonLabel .}} <code>{{reasonLabel .}}</code>{{end}}</td>{{end}}
      {{if .Resolution}}<td>{{if fixedURL .}}<a href="{{fixedURL .}}" target="_blank" rel="noopener">{{fixedLabel .}}</a>{{else if unresolvedReason .}}<span class="unresolved-reason">{{unresolvedReason .}}</span>{{end}}</td><td>{{if strategyLabel .}}<span class="badge {{strategyClass .}}">{{strategyLabel .}}</span>{{end}}</td>{{end}}
    </tr>
  {{end}}
  </tbody>
</table>
<script>
function filter() {
  const q = document.getElementById('q').value.toLowerCase();
  document.querySelectorAll('#tbl tbody tr').forEach(r => {
    r.style.display = r.textContent.toLowerCase().includes(q) ? '' : 'none';
  });
}
let sortDir = {};
function sort(col) {
  const tbody = document.querySelector('#tbl tbody');
  const rows = Array.from(tbody.rows);
  sortDir[col] = !sortDir[col];
  rows.sort((a, b) => {
    const v = a.cells[col].textContent.trim().localeCompare(b.cells[col].textContent.trim());
    return sortDir[col] ? v : -v;
  });
  rows.forEach(r => tbody.appendChild(r));
}
</script>
</body>
</html>
`))

type reportRow struct {
	types.Link
	Rel        string
	Result     *types.ValidationResult
	Resolution *types.ResolutionResult
}

func writeHTMLReport(outPath, root string, links []types.Link, results []types.ValidationResult, resolutions []types.ResolutionResult) error {
	rows := make([]reportRow, len(links))
	broken := 0
	for i, l := range links {
		rel := l.SourceFile
		if r, err := filepath.Rel(root, l.SourceFile); err == nil {
			rel = r
		}
		row := reportRow{Link: l, Rel: rel}
		if i < len(results) {
			r := results[i]
			row.Result = &r
			if !r.Valid && r.Reason != types.ReasonIgnored {
				broken++
			}
		}
		if i < len(resolutions) {
			res := resolutions[i]
			row.Resolution = &res
		}
		rows[i] = row
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return reportTmpl.Execute(f, struct {
		Root        string
		Links       []reportRow
		BrokenCount int
	}{Root: root, Links: rows, BrokenCount: broken})
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
