package resolver

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// CloneCache tracks per-run clone state: failed attempts and in-progress locks.
// Create with NewCloneCache(); pass to ensureClone calls within one run.
type CloneCache struct {
	mu     sync.Mutex
	failed map[string]bool
}

// NewCloneCache returns an empty CloneCache for a single run.
func NewCloneCache() *CloneCache {
	return &CloneCache{failed: make(map[string]bool)}
}

// ensureClone ensures a blobless clone of repoURL exists at <cacheDir>/<owner>/<repo>.
// Returns the clone path on success, empty string on failure (warning printed).
func ensureClone(repoURL, cacheDir string, cc *CloneCache) string {
	owner, repo := ownerRepo(repoURL)
	if owner == "" || repo == "" {
		return ""
	}
	dest := filepath.Join(cacheDir, owner, repo)
	return ensureCloneURL(repoURL, dest, cc)
}

// ensureCloneURL clones repoURL into dest if not already present.
func ensureCloneURL(repoURL, dest string, cc *CloneCache) string {
	// Already cloned.
	if _, err := os.Stat(filepath.Join(dest, ".git")); err == nil {
		return dest
	}

	// Skip if this URL already failed this run.
	if cc != nil {
		cc.mu.Lock()
		if cc.failed[repoURL] {
			cc.mu.Unlock()
			return ""
		}
		cc.mu.Unlock()
	}

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
		if cc != nil {
			cc.mu.Lock()
			cc.failed[repoURL] = true
			cc.mu.Unlock()
		}
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
