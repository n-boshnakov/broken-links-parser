package resolver

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// gitLogResult caches the outcome of a git log call for a single file path.
type gitLogResult struct {
	newPath     string // non-empty if file was renamed/moved
	deletionSHA string // non-empty if file was deleted
}

// GitCache holds per-run git log and fetch state.
// Create one with NewGitCache() per Resolve call; never share across calls.
type GitCache struct {
	logMu    sync.Mutex
	logCache map[string]gitLogResult

	fetchMu sync.Mutex
	fetched map[string]struct{} // only roots where fetch succeeded
}

// NewGitCache returns an empty, ready-to-use GitCache.
func NewGitCache() *GitCache {
	return &GitCache{
		logCache: make(map[string]gitLogResult),
		fetched:  make(map[string]struct{}),
	}
}

// fetch runs `git fetch --quiet origin` in repoRoot at most once per cache lifetime.
// It only marks the root as fetched when the command succeeds, allowing retries on failure.
func (c *GitCache) fetch(repoRoot string, noFetch bool) error {
	if noFetch {
		return nil
	}
	c.fetchMu.Lock()
	defer c.fetchMu.Unlock()
	if _, done := c.fetched[repoRoot]; done {
		return nil
	}
	cmd := exec.Command("git", "-C", repoRoot, "fetch", "--quiet", "origin")
	if out, err := cmd.CombinedOutput(); err != nil {
		// Do NOT mark as fetched — allow retry on next call.
		return fmt.Errorf("git fetch in %s: %w\n%s", repoRoot, err, out)
	}
	c.fetched[repoRoot] = struct{}{}
	return nil
}

// log queries rename and deletion history for path inside repoRoot.
// Results are cached to avoid redundant subprocess calls within the same run.
func (c *GitCache) log(repoRoot, path string) (gitLogResult, error) {
	key := repoRoot + ":" + path
	c.logMu.Lock()
	if r, ok := c.logCache[key]; ok {
		c.logMu.Unlock()
		return r, nil
	}
	c.logMu.Unlock()

	result, err := runGitLog(repoRoot, path)
	if err != nil {
		return gitLogResult{}, err
	}

	c.logMu.Lock()
	c.logCache[key] = result
	c.logMu.Unlock()
	return result, nil
}

func runGitLog(repoRoot, path string) (gitLogResult, error) {
	// Find the most recent commit that touched this path.
	shaCmd := exec.Command("git", "-C", repoRoot,
		"log", "--all", "--format=%H", "--max-count=1", "--", path)
	shaOut, err := shaCmd.Output()
	if err != nil || len(bytes.TrimSpace(shaOut)) == 0 {
		return gitLogResult{}, nil
	}
	sha := strings.TrimSpace(string(shaOut))

	// Inspect that commit to see if this was a rename or a deletion.
	showCmd := exec.Command("git", "-C", repoRoot,
		"show", "--name-status", "--format=", sha)
	showOut, err := showCmd.Output()
	if err != nil {
		return gitLogResult{}, nil
	}

	for _, line := range strings.Split(string(showOut), "\n") {
		parts := strings.Fields(line)
		if len(parts) == 3 && strings.HasPrefix(parts[0], "R") &&
			parts[1] == path {
			return gitLogResult{newPath: parts[2]}, nil
		}
		if len(parts) == 2 && parts[0] == "D" && parts[1] == path {
			return gitLogResult{deletionSHA: sha}, nil
		}
	}

	// The path was modified or added in that commit — not a rename or deletion.
	return gitLogResult{}, nil
}

// ResolveRelative attempts to find the correct path for a broken relative link
// by inspecting the local git history of the repository.
func ResolveRelative(result types.ValidationResult, repoRoot string, noFetch bool, cache *GitCache) types.ResolutionResult {
	// Best-effort fetch; don't fail resolution if fetch fails (e.g. offline).
	_ = cache.fetch(repoRoot, noFetch)

	// Reconstruct the absolute path of the missing target.
	url := result.Link.URL
	fragment := ""
	if i := strings.Index(url, "#"); i >= 0 {
		fragment = url[i:]
		url = url[:i]
	}
	if url == "" {
		return types.ResolutionResult{ValidationResult: result}
	}
	absTarget := filepath.Join(filepath.Dir(result.Link.SourceFile), filepath.FromSlash(url))
	// Make it relative to the repo root for git log.
	relTarget, err := filepath.Rel(repoRoot, absTarget)
	if err != nil {
		return types.ResolutionResult{ValidationResult: result}
	}

	logResult, err := cache.log(repoRoot, relTarget)
	if err != nil || (logResult.newPath == "" && logResult.deletionSHA == "") {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedNoHistory}
	}

	if logResult.newPath != "" {
		// Re-compute as a relative link from the source file.
		newAbs := filepath.Join(repoRoot, logResult.newPath)
		newRel, err := filepath.Rel(filepath.Dir(result.Link.SourceFile), newAbs)
		if err != nil {
			newRel = logResult.newPath
		}
		fixedURL := filepath.ToSlash(newRel) + fragment
		return types.ResolutionResult{
			ValidationResult: result,
			FixedURL:         fixedURL,
			Strategy:         types.StrategyGitHistory,
			ConfidenceScore:  0.95,
			Confidence:       types.ConfidenceLabel(0.95),
		}
	}

	// File was deleted — point to the deletion commit.
	remoteURL := getRemoteURL(repoRoot)
	commitURL := logResult.deletionSHA
	if remoteURL != "" {
		remoteURL = strings.TrimSuffix(remoteURL, ".git")
		commitURL = remoteURL + "/commit/" + logResult.deletionSHA
	}
	return types.ResolutionResult{
		ValidationResult: result,
		FixedURL:         commitURL,
		Strategy:         types.StrategyGitHistory,
		ConfidenceScore:  0.9,
		Confidence:       types.ConfidenceLabel(0.9),
		Deleted:          true,
	}
}

// GetRemoteURL returns the canonical remote URL for a git repo.
// Prefers "upstream" over "origin" so forks return the parent repo URL.
func GetRemoteURL(repoRoot string) string {
	return getRemoteURL(repoRoot)
}

func getRemoteURL(repoRoot string) string {
	// Prefer "upstream" (canonical repo) over "origin" (may be a fork).
	for _, remote := range []string{"upstream", "origin"} {
		cmd := exec.Command("git", "-C", repoRoot, "remote", "get-url", remote)
		out, err := cmd.Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
