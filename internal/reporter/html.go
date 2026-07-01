package reporter

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"

	"github.com/n-boshnakov/broken-links-parser/internal/pipeline"
	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// reportRow is the per-link data passed to the HTML template.
type reportRow struct {
	types.Link
	Rel        string
	Result     *types.ValidationResult
	Resolution *types.ResolutionResult
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
		if r.Resolution.IsWaybackFallback {
			return "No live replacement found — see archived version: " + r.Resolution.FixedURL
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
		case types.UnresolvedBotBlocked:
			return "Bot-blocked (403/429) — likely works in browser"
		case types.UnresolvedAIFailed:
			return "AI returned no suggestion"
		case types.UnresolvedAIAuthError:
			return "AI auth error (check AI_API_KEY)"
		case types.UnresolvedAIInvalidURL:
			return "AI returned an invalid URL"
		case types.UnresolvedAINoValidCandidate:
			return "AI suggestions did not pass validation"
		case types.UnresolvedSourceMalformed:
			return "Source URL is malformed"
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
		switch r.Resolution.Strategy {
		case types.StrategyAI:
			return "AI (low confidence)"
		case types.StrategyWaybackAI:
			return "Wayback + AI"
		}
		return r.Resolution.Strategy
	},
	"strategyClass": func(r reportRow) string {
		if r.Resolution == nil {
			return ""
		}
		switch r.Resolution.Strategy {
		case types.StrategyAI, types.StrategyWaybackAI:
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

// WriteHTML writes an interactive HTML report to outPath.
// root is used to compute relative source file paths in the report.
func WriteHTML(outPath, root string, r *pipeline.Result) error {
	rows := make([]reportRow, len(r.Links))
	broken := 0
	for i, l := range r.Links {
		rel := l.SourceFile
		if relPath, err := filepath.Rel(root, l.SourceFile); err == nil {
			rel = relPath
		}
		row := reportRow{Link: l, Rel: rel}
		if i < len(r.Validations) {
			v := r.Validations[i]
			row.Result = &v
			if !v.Valid && v.Reason != types.ReasonIgnored {
				broken++
			}
		}
		if i < len(r.Resolutions) {
			res := r.Resolutions[i]
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
