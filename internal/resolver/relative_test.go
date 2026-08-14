package resolver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// initGitRepo creates a minimal git repo in dir with a configured user.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
}

func commit(t *testing.T, dir, msg string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", ".")
	run("commit", "-m", msg)
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, _ := cmd.Output()
	return strings.TrimSpace(string(out))
}

func TestResolveRelative_Rename(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	_ = os.WriteFile(filepath.Join(dir, "old.md"), []byte("# Old\n"), 0o644)
	commit(t, dir, "add old.md")

	cmd := exec.Command("git", "-C", dir, "mv", "old.md", "new.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	commit(t, dir, "rename old.md to new.md")

	sourceFile := filepath.Join(dir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	result := types.ValidationResult{
		Link: types.Link{
			URL:        "old.md",
			Type:       types.LinkTypeRelative,
			SourceFile: sourceFile,
		},
		Valid:  false,
		Reason: types.ReasonFileNotFound,
	}

	res := ResolveRelative(result, dir, true, NewGitCache())
	if res.FixedURL == "" {
		t.Fatal("expected a FixedURL, got empty")
	}
	if !strings.Contains(res.FixedURL, "new.md") {
		t.Errorf("FixedURL %q should contain new.md", res.FixedURL)
	}
	if res.Strategy != types.StrategyGitHistory {
		t.Errorf("Strategy = %q, want %q", res.Strategy, types.StrategyGitHistory)
	}
	if res.Confidence != types.ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, types.ConfidenceHigh)
	}
}

func TestResolveRelative_TransitiveRename(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// A → B → C rename chain; the link points to A.
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n"), 0o644)
	commit(t, dir, "add a.md")
	gitMv := func(from, to string) {
		t.Helper()
		cmd := exec.Command("git", "-C", dir, "mv", from, to)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git mv %s %s: %v\n%s", from, to, err, out)
		}
	}
	gitMv("a.md", "b.md")
	commit(t, dir, "rename a.md to b.md")
	gitMv("b.md", "c.md")
	commit(t, dir, "rename b.md to c.md")

	sourceFile := filepath.Join(dir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	result := types.ValidationResult{
		Link:   types.Link{URL: "a.md", Type: types.LinkTypeRelative, SourceFile: sourceFile},
		Valid:  false,
		Reason: types.ReasonFileNotFound,
	}
	res := ResolveRelative(result, dir, true, NewGitCache())
	if res.FixedURL == "" {
		t.Fatal("expected a FixedURL, got empty")
	}
	// Must resolve to the TERMINAL path c.md, not the intermediate b.md.
	if !strings.Contains(res.FixedURL, "c.md") {
		t.Errorf("FixedURL %q should point to the terminal path c.md", res.FixedURL)
	}
	if strings.Contains(res.FixedURL, "b.md") {
		t.Errorf("FixedURL %q should NOT stop at the intermediate b.md", res.FixedURL)
	}
}

func TestResolveRelative_RenameChainEndingInDeletion(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A\n"), 0o644)
	commit(t, dir, "add a.md")
	cmd := exec.Command("git", "-C", dir, "mv", "a.md", "b.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	commit(t, dir, "rename a.md to b.md")
	// Now delete b.md.
	rm := exec.Command("git", "-C", dir, "rm", "b.md")
	if out, err := rm.CombinedOutput(); err != nil {
		t.Fatalf("git rm: %v\n%s", err, out)
	}
	commit(t, dir, "delete b.md")

	sourceFile := filepath.Join(dir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	result := types.ValidationResult{
		Link:   types.Link{URL: "a.md", Type: types.LinkTypeRelative, SourceFile: sourceFile},
		Valid:  false,
		Reason: types.ReasonFileNotFound,
	}
	res := ResolveRelative(result, dir, true, NewGitCache())
	// The chain ends in deletion; expect a deletion-commit result.
	if !res.Deleted {
		t.Errorf("expected Deleted=true for a chain ending in deletion, got FixedURL=%q Deleted=%v reason=%q",
			res.FixedURL, res.Deleted, res.UnresolvedReason)
	}
}

func TestResolveRelative_Deletion(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	_ = os.WriteFile(filepath.Join(dir, "gone.md"), []byte("# Gone\n"), 0o644)
	commit(t, dir, "add gone.md")

	_ = os.Remove(filepath.Join(dir, "gone.md"))
	deletionSHA := commit(t, dir, "delete gone.md")

	sourceFile := filepath.Join(dir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	result := types.ValidationResult{
		Link: types.Link{
			URL:        "gone.md",
			Type:       types.LinkTypeRelative,
			SourceFile: sourceFile,
		},
		Valid:  false,
		Reason: types.ReasonFileNotFound,
	}

	res := ResolveRelative(result, dir, true, NewGitCache())
	if res.FixedURL == "" {
		t.Fatal("expected a FixedURL for deletion, got empty")
	}
	if !strings.Contains(res.FixedURL, deletionSHA) {
		t.Errorf("FixedURL %q should contain deletion SHA %s", res.FixedURL, deletionSHA)
	}
	if res.Strategy != types.StrategyGitHistory {
		t.Errorf("Strategy = %q, want %q", res.Strategy, types.StrategyGitHistory)
	}
}

func TestResolveRelative_Cache(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	_ = os.WriteFile(filepath.Join(dir, "a.md"), []byte("# A file\n"), 0o644)
	commit(t, dir, "initial")
	cmd := exec.Command("git", "-C", dir, "mv", "a.md", "b.md")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git mv: %v\n%s", err, out)
	}
	commit(t, dir, "rename a to b")

	sourceFile := filepath.Join(dir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	link := types.ValidationResult{
		Link:   types.Link{URL: "a.md", Type: types.LinkTypeRelative, SourceFile: sourceFile},
		Valid:  false,
		Reason: types.ReasonFileNotFound,
	}

	cache := NewGitCache()
	ResolveRelative(link, dir, true, cache)
	ResolveRelative(link, dir, true, cache) // second call should hit cache

	cache.logMu.Lock()
	count := len(cache.logCache)
	cache.logMu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 cache entry, got %d", count)
	}
}
