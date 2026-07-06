package resolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// failedClones tracks repos that failed to clone this run so we don't retry them.
var (
	failedClonesMu sync.Mutex
	failedClones   = map[string]bool{}
)

// ensureClone ensures a blobless clone of repoURL exists at <cacheDir>/<owner>/<repo>.
// Returns the clone path on success, empty string on failure (warning printed).
func ensureClone(repoURL, cacheDir string) string {
	owner, repo := ownerRepo(repoURL)
	if owner == "" || repo == "" {
		return ""
	}
	dest := filepath.Join(cacheDir, owner, repo)
	return ensureCloneURL(repoURL, dest)
}

// ensureCloneURL clones repoURL into dest if not already present.
// Failed clone attempts are remembered for the lifetime of the process — no retries.
func ensureCloneURL(repoURL, dest string) string {
	// Already cloned.
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		return dest
	}

	// Skip if this URL already failed this run.
	failedClonesMu.Lock()
	if failedClones[repoURL] {
		failedClonesMu.Unlock()
		return ""
	}
	failedClonesMu.Unlock()

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "cache: cannot create directory for %s: %v\n", repoURL, err)
		return ""
	}
	fmt.Fprintf(os.Stderr, "Cloning %s into cache…\n", repoURL)
	cmd := exec.Command("git", "clone",
		"--filter=blob:none",
		"--no-single-branch",
		"--quiet",
		repoURL, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "cache: failed to clone %s: %v\n%s\n", repoURL, err, out)
		failedClonesMu.Lock()
		failedClones[repoURL] = true
		failedClonesMu.Unlock()
		return ""
	}
	return dest
}

// ownerRepo extracts owner and repo from a github.com URL.
func ownerRepo(repoURL string) (owner, repo string) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(repoURL, prefix) {
		return
	}
	rest := repoURL[len(prefix):]
	for i, c := range rest {
		if c == '/' {
			return rest[:i], rest[i+1:]
		}
	}
	return
}
