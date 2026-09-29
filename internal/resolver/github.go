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

// parseGitHubURL extracts owner, repo, branch, and file path from a GitHub URL.
// Supports https://<host>/owner/repo/blob/branch/path/to/file, and also
// /tree/branch/path/to/file when the path has a file extension — GitHub uses /tree/
// for directories, but authors sometimes mistype a file link as /tree/; such a link
// is still resolvable (and gets rebuilt as a correct /blob/ URL).
func parseGitHubURL(rawURL string) (owner, repo, branch, filePath string, ok bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return
	}
	parts := strings.SplitN(strings.TrimPrefix(u.Path, "/"), "/", 5)
	if len(parts) < 5 {
		return
	}
	verb := parts[2]
	// filePath does not include the fragment; u.Fragment holds it separately.
	// Trim any trailing slash that can appear when the URL is written as path/#anchor.
	path := strings.TrimRight(parts[4], "/")
	switch verb {
	case "blob":
		// A file link.
	case "tree":
		// A directory link — only treat as a file when the path clearly names a file
		// (has an extension), i.e. a mistyped /tree/ that should have been /blob/.
		if filepath.Ext(path) == "" {
			return
		}
	default:
		return
	}
	return parts[0], parts[1], parts[3], path, true
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

// buildFileIndex fetches the Git Trees API for owner/repo at the given ref and
// returns a map of basename → all full tree paths for blob entries with that
// basename. A basename can map to multiple paths (e.g. several reconciler.go),
// so callers must handle collisions rather than assume a single path.
func buildFileIndex(client *http.Client, apiBase, owner, repo, ref, token string) (map[string][]string, error) {
	if ref == "" {
		ref = "HEAD"
	}
	apiURL := fmt.Sprintf("%s/repos/%s/%s/git/trees/%s?recursive=1", apiBase, owner, repo, url.PathEscape(ref))
	return buildFileIndexFromURL(client, apiURL, token)
}

// apiError carries a GitHub API HTTP status so callers can classify failures.
type apiError struct {
	status int
}

func (e *apiError) Error() string { return fmt.Sprintf("github API returned %d", e.status) }

// buildFileIndexFromURL is the testable core of buildFileIndex.
func buildFileIndexFromURL(client *http.Client, apiURL, token string) (map[string][]string, error) {
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
		Truncated bool `json:"truncated"`
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, err
	}

	// A truncated tree means the repo is large enough that the Trees API returned a
	// partial listing; a genuine file may be absent from the index. We don't paginate
	// here, but note it so a false "no history" is at least explainable.
	if body.Truncated {
		fmt.Fprintf(os.Stderr, "github: tree listing truncated for %s — some paths may be missing from the index\n", apiURL)
	}

	index := make(map[string][]string, len(body.Tree))
	for _, entry := range body.Tree {
		if entry.Type == "blob" {
			base := filepath.Base(entry.Path)
			// Keep every occurrence so genuine same-basename collisions can be
			// detected and disambiguated by the caller.
			index[base] = append(index[base], entry.Path)
		}
	}
	return index, nil
}

// treeIndexWithFallback builds the file index for ref, retrying transient rate-limit
// errors, and falls back to HEAD when the requested ref's tree is not found (404).
func treeIndexWithFallback(client *http.Client, apiBase, owner, repo, ref, token string) (map[string][]string, error) {
	attempt := func(r string) (map[string][]string, error) {
		var index map[string][]string
		var err error
		for i := 0; i < 3; i++ {
			index, err = buildFileIndex(client, apiBase, owner, repo, r, token)
			if err == nil {
				return index, nil
			}
			if isRateLimitError(err) {
				backoff := time.Duration(1<<uint(i))*time.Second + time.Duration(rand.Intn(500))*time.Millisecond
				time.Sleep(backoff)
				continue
			}
			break
		}
		return index, err
	}

	index, err := attempt(ref)
	if err != nil && ref != "" && ref != "HEAD" {
		// The branch/ref may not exist (renamed, deleted); retry against HEAD.
		if ae, ok := err.(*apiError); ok && ae.status == http.StatusNotFound {
			return attempt("HEAD")
		}
	}
	return index, err
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
		// Not a /blob/<branch>/<file> link — e.g. a /tree/ directory, /issues/, /pull/,
		// a bare repo root, or a user profile. We can't resolve these via file history,
		// so say so rather than the misleading "no history found".
		return types.ResolutionResult{ValidationResult: result, UnresolvedReason: types.UnresolvedUnsupportedGitHubURL}
	}

	// A commit-SHA-pinned URL (e.g. /blob/<40-hex>/...) can 404 because the line range
	// changed or the file moved since that commit. Rather than give up, look the file
	// up at HEAD — it often still exists — but rebuild the fix against HEAD (not the
	// stale SHA) and report it at reduced confidence.
	shaPinned := isCommitSHA(branch)
	lookupRef := branch
	if shaPinned {
		lookupRef = "HEAD"
	}

	u, _ := url.Parse(result.Link.URL)
	webBase := "https://" + u.Host // e.g. https://github.tools.sap

	client := &http.Client{Timeout: 15 * time.Second}
	// Strip fragment before file path lookup.
	if i := strings.Index(filePath, "#"); i >= 0 {
		filePath = filePath[:i]
	}
	fileName := filepath.Base(filePath)

	index, indexErr := treeIndexWithFallback(client, apiBase, owner, repo, lookupRef, token)

	if indexErr != nil {
		return types.ResolutionResult{
			ValidationResult: result,
			UnresolvedReason: classifyAPIError(indexErr),
		}
	}

	// rebuildFix rebuilds the fixed URL for newPath, targeting HEAD for SHA-pinned
	// links (so the fix isn't pinned to the stale commit) and preserving the original
	// branch otherwise. confFull is the confidence for a normal match; SHA-pinned
	// matches are down-weighted since HEAD may differ from the pinned commit.
	rebuildFix := func(newPath string, confFull float64) types.ResolutionResult {
		score := confFull
		var fixedURL string
		if shaPinned {
			fixedURL = rebuildGitHubURLBranch(result.Link.URL, newPath, "HEAD")
			if score > 0.6 {
				score = 0.6
			}
		} else {
			fixedURL = rebuildGitHubURL(result.Link.URL, newPath)
		}
		return types.ResolutionResult{
			ValidationResult: result,
			FixedURL:         fixedURL,
			Strategy:         types.StrategyGitHubAPI,
			ConfidenceScore:  score,
			Confidence:       types.ConfidenceLabel(score),
		}
	}

	// Check for multiple files with the same name — fall back to commit history when ambiguous.
	matches := findAllPaths(index, fileName, filePath)
	switch len(matches) {
	case 0:
		// Not in current tree — check if the last commit touching this path was a rename or deletion.
		if sha := findDeletionCommit(client, apiBase, owner, repo, filePath, token); sha != "" {
			// Inspect the commit: if it's a rename, follow the chain to its terminal path.
			if newPath := findRenamedPath(client, apiBase, owner, repo, sha, filePath, token); newPath != "" {
				newPath = followRenameChain(client, apiBase, owner, repo, index, newPath, token)
				return rebuildFix(newPath, 0.95)
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
		return rebuildFix(matches[0], 0.95)
	default:
		// Multiple files share the same basename — use commit history to find the exact rename.
		if sha := findRenameCommit(client, apiBase, owner, repo, filePath, token); sha != "" {
			newPath := findRenamedPath(client, apiBase, owner, repo, sha, filePath, token)
			if newPath != "" {
				newPath = followRenameChain(client, apiBase, owner, repo, index, newPath, token)
				return rebuildFix(newPath, 0.7)
			}
		}
		// History couldn't disambiguate. Fall back to directory proximity: prefer the
		// candidate whose directory shares the longest path-segment prefix with the
		// original. Only offer it when the winner is unique — a tie is a coin-flip and
		// a wrong confident fix is worse than an honest "ambiguous".
		if best, ok := closestByDirProximity(filePath, matches); ok {
			return rebuildFix(best, 0.5)
		}
		return types.ResolutionResult{
			ValidationResult: result,
			UnresolvedReason: types.UnresolvedAmbiguous,
		}
	}
}

// closestByDirProximity picks the candidate whose directory shares the longest
// leading path-segment prefix with originalPath. It returns (path, true) only when
// a single candidate has the strictly-longest shared prefix; on a tie (or no
// candidates) it returns ("", false) so the caller can treat the case as ambiguous
// rather than guess.
func closestByDirProximity(originalPath string, candidates []string) (string, bool) {
	origSegs := strings.Split(filepath.ToSlash(filepath.Dir(originalPath)), "/")
	sharedPrefix := func(p string) int {
		segs := strings.Split(filepath.ToSlash(filepath.Dir(p)), "/")
		n := 0
		for n < len(origSegs) && n < len(segs) && origSegs[n] == segs[n] {
			n++
		}
		return n
	}
	bestPath, bestLen := "", -1
	tie := false
	for _, c := range candidates {
		if n := sharedPrefix(c); n > bestLen {
			bestLen, bestPath, tie = n, c, false
		} else if n == bestLen {
			tie = true
		}
	}
	if bestPath == "" || tie {
		return "", false
	}
	return bestPath, true
}

// findAllPaths returns all paths in the index whose basename matches fileName,
// excluding the original filePath (which is the known-broken location).
func findAllPaths(index map[string][]string, fileName, originalPath string) []string {
	var matches []string
	for _, path := range index[fileName] {
		if path != originalPath {
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

// followRenameChain follows a rename chain via the commits API to its terminal path.
// Given a first-hop newPath (from findRenamedPath), if that path isn't present in the
// current tree index it was renamed again; keep following until the path is in the
// index, no further rename is found, or maxRenameHops is reached. Returns the terminal
// path (which may be the input newPath if it can't be advanced).
func followRenameChain(client *http.Client, apiBase, owner, repo string, index map[string][]string, newPath, token string) string {
	inIndex := func(p string) bool {
		for _, paths := range index {
			for _, full := range paths {
				if full == p {
					return true
				}
			}
		}
		return false
	}
	current := newPath
	for hop := 0; hop < maxRenameHops; hop++ {
		if inIndex(current) {
			return current
		}
		sha := findRenameCommit(client, apiBase, owner, repo, current, token)
		if sha == "" {
			return current
		}
		next := findRenamedPath(client, apiBase, owner, repo, sha, current, token)
		if next == "" || next == current {
			return current
		}
		current = next
	}
	return current
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
// preserving the original scheme, host (so GitHub Enterprise hosts survive), branch,
// and fragment (anchor) if present.
func rebuildGitHubURL(original, newPath string) string {
	_, _, branch, _, ok := parseGitHubURL(original)
	if !ok {
		return original
	}
	return rebuildGitHubURLBranch(original, newPath, branch)
}

// rebuildGitHubURLBranch is rebuildGitHubURL with an explicit branch/ref override
// (used when a fix should target HEAD instead of a stale commit SHA).
func rebuildGitHubURLBranch(original, newPath, branch string) string {
	u, err := url.Parse(original)
	if err != nil {
		return original
	}
	owner, repo, _, _, ok := parseGitHubURL(original)
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
