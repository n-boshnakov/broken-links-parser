package resolver

import (
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// ResolveOptions controls resolution behaviour.
type ResolveOptions struct {
	RepoRoot      string // local root of the scanned repo (for relative link resolution)
	ReposDir      string // parent directory containing local clones (e.g. ~/Documents/GitHub)
	CacheDir      string // directory for auto-cloned repos; empty disables auto-cloning
	NoCache       bool   // when true, disables auto-cloning (falls back to API only)
	GitHubToken   string
	AI            AIConfig
	EnableAI      bool
	EnableWayback bool
	NoFetch       bool
}

// Resolve attempts to find a replacement URL for each broken ValidationResult.
// Valid results and IGNORED results are passed through unchanged.
// Each unique broken URL is resolved at most once per call; subsequent links
// with the same URL reuse the cached ResolutionResult with updated Link metadata.
func Resolve(results []types.ValidationResult, opts ResolveOptions) []types.ResolutionResult {
	cache := NewGitCache()
	cloneCache := NewCloneCache()
	parentCache := make(map[string]bool)
	resolvedByURL := make(map[string]types.ResolutionResult) // per-run resolution cache
	out := make([]types.ResolutionResult, len(results))
	for i, r := range results {
		if r.Valid || r.Reason == types.ReasonIgnored {
			out[i] = types.ResolutionResult{ValidationResult: r}
			continue
		}
		// Reuse resolution result for URLs already resolved this run.
		if cached, ok := resolvedByURL[r.Link.URL]; ok {
			reused := cached
			reused.ValidationResult = r // preserve per-link source metadata
			out[i] = reused
			continue
		}
		res := resolveOne(r, opts, cache, cloneCache, parentCache)
		resolvedByURL[r.Link.URL] = res
		out[i] = res
	}
	return out
}

func resolveOne(r types.ValidationResult, opts ResolveOptions, cache *GitCache, cc *CloneCache, parentCache map[string]bool) types.ResolutionResult {
	switch r.Link.Type {
	case types.LinkTypeRelative, types.LinkTypeAnchor, types.LinkTypeImage:
		if opts.RepoRoot != "" {
			return ResolveRelative(r, opts.RepoRoot, opts.NoFetch, cache)
		}
	case types.LinkTypeAbsolute:
		if isGitHubLink(r.Link.URL) {
			// Try local clone first; keep its result even if unresolved (it carries UnresolvedReason).
			var cloneResult types.ResolutionResult
			if opts.ReposDir != "" || opts.CacheDir != "" {
				cloneResult = ResolveViaLocalClone(r, opts.ReposDir, opts.CacheDir, opts.NoCache, opts.NoFetch, cache, cc)
				if cloneResult.FixedURL != "" {
					return cloneResult
				}
			}
			// Fall back to GitHub API; it always sets UnresolvedReason on failure.
			apiResult := ResolveViaGitHubAPI(r, opts.GitHubToken)
			if apiResult.FixedURL != "" {
				return apiResult
			}
			// Return the API result (carries the best UnresolvedReason, e.g. API_BLOCKED).
			return apiResult
		} else if opts.EnableAI && opts.AI.APIKey != "" {
			// Skip AI for bot-blocked sites (403) — the page likely exists, just blocks automated requests.
			if r.StatusCode == 403 || r.StatusCode == 429 {
				return types.ResolutionResult{
					ValidationResult: r,
					UnresolvedReason: types.UnresolvedBotBlocked,
				}
			}

			var wctx WaybackContext

			if opts.EnableWayback {
				// Fetch Wayback snapshot for context enrichment.
				snapURL, title, excerpt, found := FetchWaybackSnapshot(r.Link.URL)
				if found {
					wctx.SnapshotURL = snapURL
					wctx.Title = title
					wctx.Excerpt = excerpt
				}
				// Check if the parent site is alive (only for 404s).
				if r.StatusCode == 404 || r.Reason == types.ReasonHTTPError {
					if parentURL, live := checkParentSite(r.Link.URL, parentCache); live {
						wctx.ParentURL = parentURL
					}
				}
			}

			res := ResolveViaAI(r, opts.AI, wctx)

			// If AI found nothing and we have a Wayback snapshot, use it as fallback.
			if res.FixedURL == "" && wctx.SnapshotURL != "" {
				return types.ResolutionResult{
					ValidationResult:  r,
					FixedURL:          wctx.SnapshotURL,
					Strategy:          types.StrategyWaybackAI,
					Confidence:        types.ConfidenceLow,
					IsWaybackFallback: true,
				}
			}

			// If AI succeeded with Wayback context active, label the strategy accordingly.
			if res.FixedURL != "" && opts.EnableWayback && wctx.SnapshotURL != "" {
				res.Strategy = types.StrategyWaybackAI
			}
			return res
		} else {
			return types.ResolutionResult{
				ValidationResult: r,
				UnresolvedReason: types.UnresolvedExternalNoAI,
			}
		}
	}
	return types.ResolutionResult{ValidationResult: r}
}

func isGitHubLink(u string) bool {
	return strings.HasPrefix(u, "https://github.com/") ||
		strings.HasPrefix(u, "https://raw.githubusercontent.com/")
}
