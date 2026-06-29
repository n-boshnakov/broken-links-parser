package resolver

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestResolve_Integration(t *testing.T) {
	// Reset caches.
	gitLogCacheMu.Lock()
	gitLogCache = map[string]gitLogResult{}
	gitLogCacheMu.Unlock()
	fetchedMu.Lock()
	fetchedRoots = map[string]bool{}
	fetchedMu.Unlock()

	// Set up a temp git repo with a renamed file.
	repoDir := t.TempDir()
	initGitRepo(t, repoDir)
	_ = os.WriteFile(filepath.Join(repoDir, "moved.md"), []byte("# Moved\n"), 0o644)
	commit(t, repoDir, "add moved.md")
	cmd := exec.Command("git", "-C", repoDir, "mv", "moved.md", "current.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	commit(t, repoDir, "rename moved to current")

	sourceFile := filepath.Join(repoDir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	results := []types.ValidationResult{
		// Relative broken link — should be resolved via git history.
		{
			Link:   types.Link{URL: "moved.md", Type: types.LinkTypeRelative, SourceFile: sourceFile},
			Valid:  false,
			Reason: types.ReasonFileNotFound,
		},
		// Valid link — should pass through unchanged.
		{
			Link:  types.Link{URL: "current.md", Type: types.LinkTypeRelative, SourceFile: sourceFile},
			Valid: true,
		},
		// Absolute non-GitHub, AI disabled — unresolved.
		{
			Link:   types.Link{URL: "https://old.example.com/page", Type: types.LinkTypeAbsolute},
			Valid:  false,
			Reason: types.ReasonHTTPError,
		},
	}

	res := Resolve(results, ResolveOptions{RepoRoot: repoDir, NoFetch: true})

	if len(res) != 3 {
		t.Fatalf("expected 3 results, got %d", len(res))
	}
	// Relative broken link resolved.
	if res[0].FixedURL == "" || res[0].Strategy != types.StrategyGitHistory {
		t.Errorf("relative: FixedURL=%q Strategy=%q", res[0].FixedURL, res[0].Strategy)
	}
	// Valid link passed through.
	if !res[1].Valid || res[1].FixedURL != "" {
		t.Errorf("valid link should pass through: valid=%v fixedURL=%q", res[1].Valid, res[1].FixedURL)
	}
	// External link unresolved (AI disabled).
	if res[2].FixedURL != "" {
		t.Errorf("external unresolved: expected empty FixedURL, got %q", res[2].FixedURL)
	}
}
