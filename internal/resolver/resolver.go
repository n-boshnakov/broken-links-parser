package resolver

import (
	"net/url"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// ResolveOptions controls resolution behaviour.
type ResolveOptions struct {
	RepoRoot      string // local root of the scanned repo (for relative link resolution)
	ReposDir      string // parent directory containing local clones (e.g. ~/Documents/GitHub)
	CacheDir      string // directory for auto-cloned repos; empty disables auto-cloning
	NoCache       bool   // when true, disables auto-cloning (falls back to API only)
	GitHubToken   string            // deprecated: use GitHubTokens
	GitHubTokens  map[string]string // host → token; takes precedence over GitHubToken
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
		if isGitHubLink(r.Link.URL, opts.GitHubTokens) {
			// Try local clone first; keep its result even if unresolved (it carries UnresolvedReason).
			var cloneResult types.ResolutionResult
			if opts.ReposDir != "" || opts.CacheDir != "" {
				cloneResult = ResolveViaLocalClone(r, opts.ReposDir, opts.CacheDir, opts.NoCache, opts.NoFetch, cache, cc)
				if cloneResult.FixedURL != "" {
					return cloneResult
				}
			}
			// Fall back to GitHub API; it always sets UnresolvedReason on failure,
			// so its result carries either the fix or the best UnresolvedReason
			// (e.g. API_BLOCKED) either way.
			return ResolveViaGitHubAPI(r, opts.GitHubTokens)
		} else if opts.EnableAI && opts.AI.APIKey != "" {
			// Skip AI for cases where the page almost certainly still exists and AI
			// can't help: bot-blocked/rate-limited responses (403/429/418) and
			// transient timeouts. Sending these to the AI just wastes a request.
			if r.StatusCode == 403 || r.StatusCode == 429 || r.StatusCode == 418 {
				return types.ResolutionResult{
					ValidationResult: r,
					UnresolvedReason: types.UnresolvedBotBlocked,
				}
			}
			if r.Reason == types.ReasonTimeout {
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

// isGitHubLink returns true for github.com URLs and any host present in the token map.
func isGitHubLink(u string, tokens map[string]string) bool {
	if strings.HasPrefix(u, "https://github.com/") ||
		strings.HasPrefix(u, "https://raw.githubusercontent.com/") {
		return true
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return false
	}
	_, known := tokens[parsed.Hostname()]
	return known
}
