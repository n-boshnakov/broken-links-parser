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

var (
	gitLogCache   = map[string]gitLogResult{}
	gitLogCacheMu sync.Mutex
	fetchedRoots  = map[string]bool{}
	fetchedMu     sync.Mutex
)

// gitFetch runs `git fetch --quiet origin` in repoRoot once per root per process.
func gitFetch(repoRoot string) error {
	fetchedMu.Lock()
	defer fetchedMu.Unlock()
	if fetchedRoots[repoRoot] {
		return nil
	}
	cmd := exec.Command("git", "-C", repoRoot, "fetch", "--quiet", "origin")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git fetch in %s: %w\n%s", repoRoot, err, out)
	}
	fetchedRoots[repoRoot] = true
	return nil
}

// gitLog queries rename and deletion history for path inside repoRoot.
// Results are cached to avoid redundant subprocess calls within the same run.
func gitLog(repoRoot, path string) (gitLogResult, error) {
	key := repoRoot + ":" + path
	gitLogCacheMu.Lock()
	if r, ok := gitLogCache[key]; ok {
		gitLogCacheMu.Unlock()
		return r, nil
	}
	gitLogCacheMu.Unlock()

	result, err := runGitLog(repoRoot, path)
	if err != nil {
		return gitLogResult{}, err
	}

	gitLogCacheMu.Lock()
	gitLogCache[key] = result
	gitLogCacheMu.Unlock()
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
func ResolveRelative(result types.ValidationResult, repoRoot string, noFetch bool) types.ResolutionResult {
	if !noFetch {
		// Best-effort fetch; don't fail resolution if fetch fails (e.g. offline).
		_ = gitFetch(repoRoot)
	}

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

	logResult, err := gitLog(repoRoot, relTarget)
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
			Confidence:       types.ConfidenceHigh,
		}
	}

	// File was deleted — point to the deletion commit.
	// We need the remote URL to build a commit link; use git remote get-url.
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
		Confidence:       types.ConfidenceHigh,
		Deleted:          true,
	}
}

func getRemoteURL(repoRoot string) string {
	cmd := exec.Command("git", "-C", repoRoot, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
