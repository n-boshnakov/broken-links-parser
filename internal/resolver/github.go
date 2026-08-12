package resolver

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// apiBaseForHost returns the GitHub REST API base URL for the given hostname.
func apiBaseForHost(host string) string {
	if host == "github.com" {
		return "https://api.github.com"
	}
	return "https://" + host + "/api/v3"
}

// parseGitHubURL extracts owner, repo, branch, file path, and fragment from a GitHub URL.
// Supports: https://<host>/owner/repo/blob/branch/path/to/file#anchor
func parseGitHubURL(rawURL string) (owner, repo, branch, filePath string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 5)
	if len(parts) < 5 || parts[2] != "blob" {
		return
	}
	// filePath does not include the fragment; u.Fragment holds it separately.
	// Trim any trailing slash that can appear when the URL is written as path/#anchor.
	filePath = strings.TrimRight(parts[4], "/")
	return parts[0], parts[1], parts[3], filePath, true
}

// findLocalClone checks reposDir, then cacheDir, for a valid git repo matching owner/repo.
// If cacheDir is non-empty and noCache is false and the repo is not found, it auto-clones it.
// Returns the repo root path and whether it was found/cloned.
func findLocalClone(reposDir, cacheDir, owner, repo string, noCache bool, cc *CloneCache) (string, bool) {
	// 1. Check reposDir first (user-managed clones take priority).
	if reposDir != "" {
		for _, p := range []string{
			filepath.Join(reposDir, owner, repo),
			filepath.Join(reposDir, repo),
		} {
			if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
				return p, true
			}
		}
	}
	// 2. Check cache directory.
	if cacheDir != "" {
		p := filepath.Join(cacheDir, owner, repo)
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return p, true
		}
		// 3. Auto-clone if not found and cache is enabled.
		if !noCache {
			repoURL := "https://github.com/" + owner + "/" + repo
			if cloned := ensureClone(repoURL, cacheDir, cc); cloned != "" {
				return cloned, true
			}
		}
	}
	return "", false
}

// ResolveViaLocalClone resolves a broken absolute GitHub link using a local clone.
func ResolveViaLocalClone(result types.ValidationResult, reposDir, cacheDir string, noCache, noFetch bool, cache *GitCache, cc *CloneCache) types.ResolutionResult {
	owner, repo, _, filePath, ok := parseGitHubURL(result.Link.URL)
	if !ok {
		return types.ResolutionResult{ValidationResult: result}
	}

	// Strip fragment — git log only operates on file paths.
	if i := strings.Index(filePath, "#"); i >= 0 {
		filePath = filePath[:i]
	}

	clonePath, found := findLocalClone(reposDir, cacheDir, owner, repo, noCache, cc)
	if !found {
		return types.ResolutionResult{ValidationResult: result}
	}

	_ = cache.fetch(clonePath, noFetch)

	logResult, err := cache.log(clonePath, filePath)
	if err != nil || (logResult.newPath == "" && logResult.deletionSHA == "") {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedNoHistory}
	}

	if logResult.newPath != "" {
		// Rebuild the absolute GitHub URL with the new path.
		fixedURL := rebuildGitHubURL(result.Link.URL, logResult.newPath)
		return types.ResolutionResult{
			ValidationResult: result,
			FixedURL:         fixedURL,
			Strategy:         types.StrategyLocalClone,
			ConfidenceScore:  0.95,
			Confidence:       types.ConfidenceLabel(0.95),
		}
	}

	remoteURL := getRemoteURL(clonePath)
	commitURL := logResult.deletionSHA
	if remoteURL != "" {
		remoteURL = strings.TrimSuffix(remoteURL, ".git")
		commitURL = remoteURL + "/commit/" + logResult.deletionSHA
	}
	return types.ResolutionResult{
		ValidationResult: result,
		FixedURL:         commitURL,
		Strategy:         types.StrategyLocalClone,
		ConfidenceScore:  0.9,
		Confidence:       types.ConfidenceLabel(0.9),
		Deleted:          true,
	}
}

// buildFileIndex fetches the Git Trees API for owner/repo and returns a map
// of basename → full tree path for all blob entries.
func buildFileIndex(client *http.Client, apiBase, owner, repo, token string) (map[string]string, error) {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/HEAD?recursive=1", apiBase, owner, repo)
	return buildFileIndexFromURL(client, apiURL, token)
}

// apiError carries a GitHub API HTTP status so callers can classify failures.
type apiError struct {
	status int
}

func (e *apiError) Error() string { return fmt.Sprintf("github API returned %d", e.status) }

// buildFileIndexFromURL is the testable core of buildFileIndex.
func buildFileIndexFromURL(client *http.Client, apiURL, token string) (map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &apiError{status: resp.StatusCode}
	}

	var body struct {
		Tree []struct {
			Path string `json:"path"`
			Type string `json:"type"`
		} `json:"tree"`
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}

	index := make(map[string]string, len(body.Tree))
	for _, entry := range body.Tree {
		if entry.Type == "blob" {
			base := filepath.Base(entry.Path)
			// Keep first occurrence to avoid clobbering with a later duplicate name.
			if _, exists := index[base]; !exists {
				index[base] = entry.Path
			}
		}
	}
	return index, nil
}

// ResolveViaGitHubAPI resolves a broken absolute GitHub link using the GitHub API.
func ResolveViaGitHubAPI(result types.ValidationResult, tokens map[string]string) types.ResolutionResult {
	u, err := url.Parse(result.Link.URL)
	if err != nil {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedNoHistory}
	}
	host := u.Hostname()
	token := tokens[host]
	apiBase := apiBaseForHost(host)
	return resolveViaGitHubAPIWithBase(result, token, apiBase)
}

func resolveViaGitHubAPIWithBase(result types.ValidationResult, token, apiBase string) types.ResolutionResult {
	owner, repo, branch, filePath, ok := parseGitHubURL(result.Link.URL)
	if !ok {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedNoHistory}
	}

	// If the URL references a specific commit SHA (40 hex chars), the file at that
	// commit may legitimately return 404 because the line range has changed or the
	// file was renamed since. Don't report this as "deleted" — it's a pinned reference.
	if isCommitSHA(branch) {
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedNoHistory}
	}

	u, _ := url.Parse(result.Link.URL)
	webBase := "https://" + u.Host // e.g. https://github.tools.sap

	client := &http.Client{Timeout: 15 * time.Second}
	// Strip fragment before file path lookup.
	if i := strings.Index(filePath, "#"); i >= 0 {
		filePath = filePath[:i]
	}
	fileName := filepath.Base(filePath)
	apiURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/HEAD?recursive=1", apiBase, owner, repo)

	var index map[string]string
	var indexErr error
	for attempt := 0; attempt < 3; attempt++ {
		index, indexErr = buildFileIndexFromURL(client, apiURL, token)
		if indexErr == nil {
			break
		}
		if isRateLimitError(indexErr) {
			backoff := time.Duration(1<<uint(attempt))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
			time.Sleep(backoff)
			continue
		}
		break
	}

	if indexErr != nil {
		return types.ResolutionResult{
			ValidationResult: result,
			UnresolvedReason: classifyAPIError(indexErr),
		}
	}

	// Check for multiple files with the same name — fall back to commit history when ambiguous.
	matches := findAllPaths(index, fileName, filePath)
	switch len(matches) {
	case 0:
		// Not in current tree — check if the last commit touching this path was a rename or deletion.
		if sha := findDeletionCommit(client, apiBase, owner, repo, filePath, token); sha != "" {
			// Inspect the commit: if it's a rename, return the new path as the fix.
			if newPath := findRenamedPath(client, apiBase, owner, repo, sha, filePath, token); newPath != "" {
				fixedURL := rebuildGitHubURL(result.Link.URL, newPath)
				return types.ResolutionResult{
					ValidationResult: result,
					FixedURL:         fixedURL,
					Strategy:         types.StrategyGitHubAPI,
					ConfidenceScore:  0.95,
					Confidence:       types.ConfidenceLabel(0.95),
				}
			}
			// Commit exists but was a deletion, not a rename.
			fixedURL := fmt.Sprintf("%s/%s/%s/commit/%s", webBase, owner, repo, sha)
			return types.ResolutionResult{
				ValidationResult: result,
				FixedURL:         fixedURL,
				Strategy:         types.StrategyGitHubAPI,
				ConfidenceScore:  0.9,
				Confidence:       types.ConfidenceLabel(0.9),
				Deleted:          true,
			}
		}
		return types.ResolutionResult{
			ValidationResult: result,
			UnresolvedReason: types.UnresolvedNoHistory,
		}
	case 1:
		fixedURL := rebuildGitHubURL(result.Link.URL, matches[0])
		return types.ResolutionResult{
			ValidationResult: result,
			FixedURL:         fixedURL,
			Strategy:         types.StrategyGitHubAPI,
			ConfidenceScore:  0.95,
			Confidence:       types.ConfidenceLabel(0.95),
		}
	default:
		// Multiple files share the same basename — use commit history to find the exact rename.
		if sha := findRenameCommit(client, apiBase, owner, repo, filePath, token); sha != "" {
			newPath := findRenamedPath(client, apiBase, owner, repo, sha, filePath, token)
			if newPath != "" {
				fixedURL := rebuildGitHubURL(result.Link.URL, newPath)
				return types.ResolutionResult{
					ValidationResult: result,
					FixedURL:         fixedURL,
					Strategy:         types.StrategyGitHubAPI,
					ConfidenceScore:  0.7,
					Confidence:       types.ConfidenceLabel(0.7),
				}
			}
		}
		return types.ResolutionResult{
			ValidationResult: result,
			UnresolvedReason: types.UnresolvedAmbiguous,
		}
	}
}

// findAllPaths returns all paths in the index whose basename matches fileName,
// excluding the original filePath (which is the known-broken location).
func findAllPaths(index map[string]string, fileName, originalPath string) []string {
	var matches []string
	for base, path := range index {
		if base == fileName && path != originalPath {
			matches = append(matches, path)
		}
	}
	return matches
}

func classifyAPIError(err error) string {
	if ae, ok := err.(*apiError); ok {
		switch ae.status {
		case 403:
			return types.UnresolvedAPIBlocked
		case 404:
			return types.UnresolvedRepoNotFound
		case 429:
			return types.UnresolvedAPIRateLimit
		}
	}
	if isRateLimitError(err) {
		return types.UnresolvedAPIRateLimit
	}
	return types.UnresolvedNoHistory
}

// findRenameCommit returns the SHA of the last commit that touched filePath.
// Same as findDeletionCommit — both just need the last touching commit.
func findRenameCommit(client *http.Client, apiBase, owner, repo, filePath, token string) string {
	return findDeletionCommit(client, apiBase, owner, repo, filePath, token)
}

// findRenamedPath inspects a commit's files via the GitHub API and returns the new path
// if the commit contains a rename from oldPath.
func findRenamedPath(client *http.Client, apiBase, owner, repo, sha, oldPath, token string) string {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/commits/%s", apiBase, owner, repo, sha)
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()

	var body struct {
		Files []struct {
			Status   string `json:"status"`
			Filename string `json:"filename"`
			Previous string `json:"previous_filename"`
		} `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return ""
	}
	for _, f := range body.Files {
		if f.Status == "renamed" && f.Previous == oldPath {
			return f.Filename
		}
	}
	return ""
}

// findDeletionCommit queries the GitHub commits API for the last commit that touched
// the given file path and returns its SHA (the deletion commit).
func findDeletionCommit(client *http.Client, apiBase, owner, repo, filePath, token string) string {
	apiURL := fmt.Sprintf("%s/repos/%s/%s/commits?path=%s&per_page=1",
		apiBase, owner, repo, url.QueryEscape(filePath))
	req, err := http.NewRequest(http.MethodGet, apiURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()
	var commits []struct {
		SHA string `json:"sha"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil || len(commits) == 0 {
		return ""
	}
	return commits[0].SHA
}

func isRateLimitError(err error) bool {	return strings.Contains(err.Error(), "429") || strings.Contains(err.Error(), "403")
}

// isCommitSHA returns true if s looks like a full 40-character git commit SHA.
func isCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// rebuildGitHubURL replaces the file path portion of a GitHub blob URL with newPath,
// preserving the original scheme, host (so GitHub Enterprise hosts survive), and
// fragment (anchor) if present.
func rebuildGitHubURL(original, newPath string) string {
	u, err := url.Parse(original)
	if err != nil {
		return original
	}
	owner, repo, branch, _, ok := parseGitHubURL(original)
	if !ok {
		return original
	}
	scheme := u.Scheme
	if scheme == "" {
		scheme = "https"
	}
	rebuilt := fmt.Sprintf("%s://%s/%s/%s/blob/%s/%s", scheme, u.Host, owner, repo, branch, newPath)
	if u.Fragment != "" {
		rebuilt += "#" + u.Fragment
	}
	return rebuilt
}
