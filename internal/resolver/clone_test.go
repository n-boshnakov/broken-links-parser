package resolver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureCloneURL(t *testing.T) {
	// Create a local bare repo to avoid network access.
	srcDir := t.TempDir()
	if out, err := exec.Command("git", "init", "--bare", "-q", srcDir).CombinedOutput(); err != nil {
		t.Skipf("git init failed: %v\n%s", err, out)
	}

	dest := filepath.Join(t.TempDir(), "owner", "repo")

	// First call: should clone.
	cloned := ensureCloneURL("file://"+srcDir, dest)
	if cloned == "" {
		t.Skip("git clone not available in test environment")
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); err != nil {
		t.Errorf("clone did not create .git: %v", err)
	}

	// Second call: idempotent — returns dest without re-cloning.
	cloned2 := ensureCloneURL("file://"+srcDir, dest)
	if cloned2 != dest {
		t.Errorf("second call = %q, want %q", cloned2, dest)
	}
}

func TestOwnerRepo(t *testing.T) {
	cases := []struct{ url, owner, repo string }{
		{"https://github.com/gardener/gardener", "gardener", "gardener"},
		{"https://github.com/gardener/machine-controller-manager", "gardener", "machine-controller-manager"},
		{"https://github.com/", "", ""},
		{"https://example.com/foo/bar", "", ""},
	}
	for _, c := range cases {
		o, r := ownerRepo(c.url)
		if o != c.owner || r != c.repo {
			t.Errorf("ownerRepo(%q) = %q, %q; want %q, %q", c.url, o, r, c.owner, c.repo)
		}
	}
}
