package reporter

import (
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/pipeline"
	"github.com/n-boshnakov/broken-links-parser/internal/resolver"
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
	"isGitHubURL": func(s string) bool {
		// The source-file column holds either a full remote URL (github.com or a
		// GitHub Enterprise host) or a relative path; treat any http(s) value as a
		// clickable link.
		return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
	},
	"statusClass": func(r reportRow) string {
		if r.Result == nil {
			return ""
		}
		if r.Result.NotAssembled {
			return "not-assembled"
		}
		if r.Result.Reason == types.ReasonIgnored {
			return "ignored"
		}
		if r.Result.Valid {
			return "valid"
		}
		if r.Result.Reason == types.ReasonAuthBlocked || r.Result.Reason == types.ReasonNetworkError {
			return "warning"
		}
		return "broken"
	},
	"statusLabel": func(r reportRow) string {
		if r.Result == nil {
			return ""
		}
		if r.Result.NotAssembled {
			return "Valid — not in manifest"
		}
		if r.Result.Reason == types.ReasonIgnored {
			return "Ignored"
		}
		if r.Result.Valid {
			return "Valid"
		}
		switch r.Result.Reason {
		case types.ReasonAuthBlocked:
			return "Access restricted"
		case types.ReasonNetworkError:
			return "Network error"
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
	"suggestedFix": func(r reportRow) string {
		if r.Result == nil || r.Result.SuggestedAnchor == "" {
			return ""
		}
		// Replace the fragment in the original URL with the suggested anchor.
		u := r.URL
		if i := strings.Index(u, "#"); i >= 0 {
			u = u[:i]
		}
		return u + "#" + r.Result.SuggestedAnchor
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
		// For anchor suggestions, show the corrected relative/anchor form as the label
		// (the href is the full GitHub URL for easy navigation, but the label shows what
		// the link should look like in the source file).
		if r.Resolution.Strategy == "anchor-suggestion" && r.Result != nil && r.Result.SuggestedAnchor != "" {
			u := r.URL
			if i := strings.Index(u, "#"); i >= 0 {
				u = u[:i]
			}
			return u + "#" + r.Result.SuggestedAnchor
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
		case types.UnresolvedNoCloneNoAPI:
			return "No local clone and API unavailable"
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
		case types.UnresolvedUnsupportedGitHubURL:
			return "Not a resolvable file link"
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
		// Prefer the numeric score; fall back to the stored label.
		conf := r.Resolution.Confidence
		if r.Resolution.ConfidenceScore > 0 {
			conf = types.ConfidenceLabel(r.Resolution.ConfidenceScore)
		}
		switch r.Resolution.Strategy {
		case types.StrategyAI:
			if conf != "" {
				return "AI (" + conf + " confidence)"
			}
			return "AI"
		case types.StrategyWaybackAI:
			if conf != "" {
				return "Wayback + AI (" + conf + " confidence)"
			}
			return "Wayback + AI"
		case "anchor-suggestion":
			if conf != "" {
				return "Closest match (" + conf + " confidence)"
			}
			return "Closest match"
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
	// reasoning returns the AI's explanation for the chosen fix, shown as hover text.
	"reasoning": func(r reportRow) string {
		if r.Resolution == nil {
			return ""
		}
		return r.Resolution.Reasoning
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
  input[type=search] { padding: .4rem .6rem; font-size: 1rem; width: 22rem; }
  .filters { display:flex; flex-wrap:wrap; gap:.5rem; align-items:center; margin-bottom:1rem; }
  .filter-group { display:flex; align-items:center; gap:.25rem; font-size:.85rem; color:#555; }
  .filter-group label { font-weight:600; }
  .chip { display:inline-flex; align-items:center; padding:.2rem .6rem; border-radius:999px; border:1.5px solid #ccc; background:#fff; font-size:.78rem; font-weight:600; cursor:pointer; user-select:none; transition:border-color .15s, background .15s; }
  .chip:hover { border-color:#888; }
  .chip.active { border-color:#2563eb; background:#dbeafe; color:#1e40af; }
  .chip.active.broken-chip { border-color:#991b1b; background:#fee2e2; color:#991b1b; }
  .chip.active.valid-chip { border-color:#166534; background:#dcfce7; color:#166534; }
  .chip.active.ignored-chip { border-color:#6b7280; background:#f3f4f6; color:#6b7280; }
  .chip.active.not-assembled-chip { border-color:#92400e; background:#fef3c7; color:#92400e; }
  .chip-clear { padding:.2rem .5rem; border-radius:4px; border:1px solid #ddd; background:#f9f9f9; font-size:.78rem; cursor:pointer; color:#555; }
  .chip-clear:hover { background:#f0f0f0; }
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
  .warning  { background:#fef3c7; color:#92400e; }
  .ignored  { background:#f3f4f6; color:#6b7280; }
  .not-assembled { background:#fef3c7; color:#92400e; }
  .strategy-normal { background:#dbeafe; color:#1e40af; }
  .strategy-ai     { background:#fef9c3; color:#854d0e; }
  .unresolved-reason { color:#6b7280; font-size:.8rem; font-style:italic; }
  a { color: #2563eb; }
</style>
</head>
<body>
<h1>Link Report</h1>
<p class="meta">Root: <code>{{.Root}}</code> &nbsp;·&nbsp; {{len .Links}} links found{{if .BrokenCount}} &nbsp;·&nbsp; <strong style="color:#991b1b">{{.BrokenCount}} broken</strong>{{end}}</p>

<div class="filters">
  <div class="filter-group">
    <label>Type:</label>
    <span class="chip" onclick="toggleChip(this,'type','relative')">relative</span>
    <span class="chip" onclick="toggleChip(this,'type','absolute')">absolute</span>
    <span class="chip" onclick="toggleChip(this,'type','anchor')">anchor</span>
    <span class="chip" onclick="toggleChip(this,'type','image')">image</span>
  </div>
  {{if hasValidation .Links}}
  <div class="filter-group">
    <label>Status:</label>
    <span class="chip broken-chip" onclick="toggleChip(this,'status','broken')">Broken</span>
    <span class="chip valid-chip" onclick="toggleChip(this,'status','valid')">Valid</span>
    <span class="chip ignored-chip" onclick="toggleChip(this,'status','ignored')">Ignored</span>
    <span class="chip not-assembled-chip" onclick="toggleChip(this,'status','not-assembled')">Not in manifest</span>
  </div>
  {{end}}
  {{if hasResolution .Links}}
  <div class="filter-group">
    <label>Fixed:</label>
    <span class="chip" onclick="toggleChip(this,'fixed','yes')">Has fix</span>
    <span class="chip" onclick="toggleChip(this,'fixed','no')">Unresolved</span>
    <span class="chip" onclick="toggleChip(this,'fixed','deleted')">Deleted</span>
    <span class="chip" onclick="toggleChip(this,'fixed','wayback')">Wayback fallback</span>
  </div>
  {{end}}
  <input type="search" id="q" placeholder="Filter by URL or source…" oninput="applyFilters()">
  <button class="chip-clear" onclick="clearAll()">Clear all</button>
</div>
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
    <tr{{if eq (statusClass .) "broken"}} class="row-broken"{{end}}
        data-type="{{.Type}}"
        data-status="{{statusClass .}}"
        data-fixed="{{if fixedURL .}}{{if .Resolution}}{{if .Resolution.Deleted}}deleted{{else if .Resolution.IsWaybackFallback}}wayback{{else}}yes{{end}}{{else}}yes{{end}}{{else}}no{{end}}">
      <td><span class="badge {{.Type}}">{{.Type}}</span></td>
      <td>{{if isAbsolute .}}<a href="{{.URL}}" target="_blank" rel="noopener">{{.URL}}</a>{{else}}{{.URL}}{{end}}</td>
      <td>{{if isGitHubURL .Rel}}<a href="{{.Rel}}" target="_blank" rel="noopener">{{.Rel}}</a>{{else}}{{.Rel}}{{end}}</td>
      {{if .Result}}<td><span class="badge {{statusClass .}}">{{statusLabel .}}</span>{{if reasonLabel .}} <code>{{reasonLabel .}}</code>{{end}}</td>{{end}}
      {{if .Resolution}}<td>{{if fixedURL .}}<a href="{{fixedURL .}}" target="_blank" rel="noopener">{{fixedLabel .}}</a>{{else if unresolvedReason .}}<span class="unresolved-reason">{{unresolvedReason .}}</span>{{end}}</td><td>{{if strategyLabel .}}<span class="badge {{strategyClass .}}"{{if reasoning .}} title="{{reasoning .}}"{{end}}>{{strategyLabel .}}</span>{{end}}</td>{{end}}
    </tr>
  {{end}}
  </tbody>
</table>
<script>
const active = { type: new Set(), status: new Set(), fixed: new Set() };

function toggleChip(el, group, val) {
  if (active[group].has(val)) { active[group].delete(val); el.classList.remove('active'); }
  else { active[group].add(val); el.classList.add('active'); }
  applyFilters();
}

function applyFilters() {
  const q = document.getElementById('q').value.toLowerCase();
  document.querySelectorAll('#tbl tbody tr').forEach(r => {
    const matchType   = active.type.size   === 0 || active.type.has(r.dataset.type);
    const matchStatus = active.status.size === 0 || active.status.has(r.dataset.status);
    const matchFixed  = active.fixed.size  === 0 || active.fixed.has(r.dataset.fixed);
    const matchText   = !q || r.textContent.toLowerCase().includes(q);
    r.style.display = (matchType && matchStatus && matchFixed && matchText) ? '' : 'none';
  });
}

function clearAll() {
  ['type','status','fixed'].forEach(g => active[g].clear());
  document.querySelectorAll('.chip.active').forEach(c => c.classList.remove('active'));
  document.getElementById('q').value = '';
  applyFilters();
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
// root is used to compute source file GitHub URLs in the report.
// rootRelativeBase is the content root for resolving root-relative ("/…") links in
// anchor suggestions; pass "" when not configured.
func WriteHTML(outPath, root, rootRelativeBase string, r *pipeline.Result) error {
	// Content root relative to the repo root (e.g. "hugo/content"), for resolving
	// root-relative anchor-suggestion URLs the same way the validator does. Empty when
	// unset or equal to root.
	contentRel := ""
	if rootRelativeBase != "" {
		if rel, err := filepath.Rel(root, rootRelativeBase); err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			contentRel = filepath.ToSlash(rel)
		}
	}

	// Resolve the remote URL and default branch for the primary scanned repo.
	primaryRemote := strings.TrimSuffix(resolver.GetRemoteURL(root), ".git")
	primaryBranch := getDefaultBranch(root)

	// Build a reverse map: local file path → GitHub blob URL, for docforge-sourced files.
	// Detect branch per clone to handle repos using main vs master.
	localToGitHubURL := make(map[string]string)
	cloneBranchCache := make(map[string]string) // clone path → branch
	if r.SourceMap != nil {
		for _, entry := range r.SourceMap {
			if entry.LocalFilePath == "" || entry.RepoURL == "" || entry.RepoFilePath == "" {
				continue
			}
			branch, ok := cloneBranchCache[entry.RepoLocalClone]
			if !ok {
				branch = getDefaultBranch(entry.RepoLocalClone)
				cloneBranchCache[entry.RepoLocalClone] = branch
			}
			localToGitHubURL[entry.LocalFilePath] = entry.RepoURL + "/blob/" + branch + "/" + entry.RepoFilePath
		}
	}

	rows := make([]reportRow, len(r.Links))
	broken := 0
	for i, l := range r.Links {
		var sourceURL string
		if l.SourceRepo != "" {
			// Docforge-sourced file — use SourceMap lookup.
			if ghURL, ok := localToGitHubURL[l.SourceFile]; ok {
				sourceURL = ghURL
			} else {
				// Fallback: build from clone's remote URL with detected branch.
				cloneRemote := strings.TrimSuffix(resolver.GetRemoteURL(l.SourceRepo), ".git")
				if cloneRemote != "" {
					branch, ok := cloneBranchCache[l.SourceRepo]
					if !ok {
						branch = getDefaultBranch(l.SourceRepo)
						cloneBranchCache[l.SourceRepo] = branch
					}
					if rel, err := filepath.Rel(l.SourceRepo, l.SourceFile); err == nil {
						sourceURL = cloneRemote + "/blob/" + branch + "/" + filepath.ToSlash(rel)
					}
				}
			}
		} else if primaryRemote != "" {
			// Local repo file — build from primary repo remote.
			if rel, err := filepath.Rel(root, l.SourceFile); err == nil {
				sourceURL = primaryRemote + "/blob/" + primaryBranch + "/" + filepath.ToSlash(rel)
			}
		}
		if sourceURL == "" {
			// Last resort: relative path.
			if rel, err := filepath.Rel(root, l.SourceFile); err == nil {
				sourceURL = rel
			} else {
				sourceURL = l.SourceFile
			}
		}
		row := reportRow{Link: l, Rel: sourceURL}
		if i < len(r.Validations) {
			v := r.Validations[i]
			row.Result = &v
			if v.IsBroken() {
				broken++
			}
			// When an anchor suggestion exists but no resolution was run,
			// synthesise a resolution result so the suggestion appears in Fixed Link.
			if v.SuggestedAnchor != "" && (i >= len(r.Resolutions) || r.Resolutions[i].FixedURL == "") {
				u := l.URL
				if idx := strings.Index(u, "#"); idx >= 0 {
					u = u[:idx]
				}
				suggested := u + "#" + v.SuggestedAnchor
				// If we have a GitHub source URL, resolve the relative suggestion to an
				// absolute GitHub URL so the user can click through directly. Root-relative
				// ("/…") targets need the content root, which only applies to the primary
				// tree — pass contentRel only for primary-repo rows (empty for docforge).
				if strings.HasPrefix(sourceURL, "https://github.com/") {
					cr := contentRel
					if l.SourceRepo != "" {
						cr = ""
					}
					if abs := resolveGitHubAnchorURL(sourceURL, u, v.SuggestedAnchor, cr); abs != "" {
						suggested = abs
					}
				}
				synth := types.ResolutionResult{
					ValidationResult: v,
					FixedURL:         suggested,
					Strategy:         "anchor-suggestion",
					ConfidenceScore:  v.SuggestedAnchorScore,
					Confidence:       types.ConfidenceLabel(v.SuggestedAnchorScore),
				}
				row.Resolution = &synth
			}
		}
		if i < len(r.Resolutions) {
			res := r.Resolutions[i]
			// Only overwrite the anchor-suggestion synth when the resolution actually
			// has a fixed URL, or when there is no anchor suggestion to show.
			if res.FixedURL != "" || row.Resolution == nil {
				row.Resolution = &res
			}
		}
		rows[i] = row
	}

	// Deduplicate: if the same (sourceURL, link URL) pair appears multiple times — e.g. a
	// self-referential anchor used repeatedly in one file — keep only the first row.
	seen := make(map[string]bool, len(rows))
	deduped := rows[:0]
	for _, row := range rows {
		key := row.Rel + "\x00" + row.URL
		if seen[key] {
			if row.Result != nil && row.Result.IsBroken() {
				broken-- // undo the broken count increment for this duplicate
			}
			continue
		}
		seen[key] = true
		deduped = append(deduped, row)
	}
	rows = deduped

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

// getDefaultBranch returns the default branch name (e.g. "main" or "master") for a git repo.
// Falls back to "master" if it cannot be determined.
func getDefaultBranch(repoRoot string) string {
	// Try: git symbolic-ref refs/remotes/origin/HEAD → refs/remotes/origin/main
	cmd := exec.Command("git", "-C", repoRoot, "symbolic-ref", "refs/remotes/origin/HEAD")
	out, err := cmd.Output()
	if err == nil {
		ref := strings.TrimSpace(string(out))
		if parts := strings.Split(ref, "/"); len(parts) > 0 {
			return parts[len(parts)-1]
		}
	}
	// Fallback: check if main exists.
	cmd2 := exec.Command("git", "-C", repoRoot, "show-ref", "--verify", "--quiet", "refs/remotes/origin/main")
	if cmd2.Run() == nil {
		return "main"
	}
	return "master"
}

// resolveGitHubAnchorURL builds an absolute GitHub URL for an anchor suggestion.
// sourceURL is the GitHub blob URL of the file containing the broken link.
// relTarget is the relative path portion of the broken link (without fragment).
// anchor is the suggested anchor fragment.
// contentRel is the content root's path relative to the repo root (e.g. "hugo/content"),
// used to resolve root-relative ("/…") targets the same way the validator does; pass ""
// when unknown, in which case root-relative targets return "" (no misleading URL).
// Returns "" if sourceURL is not a recognised GitHub URL.
func resolveGitHubAnchorURL(sourceURL, relTarget, anchor, contentRel string) string {
	// sourceURL: https://github.com/owner/repo/blob/branch/path/to/source.md
	// Strip fragment and trailing slash from sourceURL first.
	base := sourceURL
	if i := strings.Index(base, "#"); i >= 0 {
		base = base[:i]
	}
	// Split into prefix (https://github.com/owner/repo/blob/branch) and file path.
	// Format: https://github.com/owner/repo/blob/branch/path/to/file
	const ghPrefix = "https://github.com/"
	if !strings.HasPrefix(base, ghPrefix) {
		return ""
	}
	parts := strings.SplitN(strings.TrimPrefix(base, ghPrefix), "/", 5)
	// parts: [owner, repo, "blob", branch, path/to/source.md]
	if len(parts) < 5 || parts[2] != "blob" {
		return ""
	}
	repoBase := ghPrefix + parts[0] + "/" + parts[1] + "/blob/" + parts[3] + "/"
	sourcePath := parts[4] // e.g. "docs/usage/security/shoot_serviceaccounts.md"

	// Anchor-only link — target is the source file itself.
	if relTarget == "" {
		return repoBase + sourcePath + "#" + anchor
	}

	// Root-relative target ("/docs/...") resolves against the content root, not the
	// source file's directory — mirroring the validator (relative.go). Without a known
	// content root we can't map it correctly, so return "" and let the caller fall back
	// to the plain suggested form rather than emit a wrong URL.
	if strings.HasPrefix(relTarget, "/") {
		if contentRel == "" {
			return ""
		}
		resolved := strings.TrimSuffix(contentRel, "/") + "/" + strings.TrimPrefix(relTarget, "/")
		return repoBase + cleanJoinedPath(resolved) + "#" + anchor
	}

	// Otherwise resolve relTarget relative to the directory of sourcePath.
	dir := sourcePath
	if i := strings.LastIndex(dir, "/"); i >= 0 {
		dir = dir[:i+1]
	} else {
		dir = ""
	}
	return repoBase + cleanJoinedPath(dir+relTarget) + "#" + anchor
}

// cleanJoinedPath collapses "." and ".." segments and drops empty segments from a
// slash-separated path, returning the normalised path.
func cleanJoinedPath(p string) string {
	segs := strings.Split(p, "/")
	var clean []string
	for _, s := range segs {
		switch s {
		case ".":
			// skip
		case "..":
			if len(clean) > 0 {
				clean = clean[:len(clean)-1]
			}
		default:
			if s != "" {
				clean = append(clean, s)
			}
		}
	}
	return strings.Join(clean, "/")
}
