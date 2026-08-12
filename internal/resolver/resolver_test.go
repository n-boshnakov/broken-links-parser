package resolver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestResolve_Integration(t *testing.T) {
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

func TestResolve_WaybackFallback(t *testing.T) {
	// A broken external (non-GitHub) absolute link.
	result := types.ValidationResult{
		Link:       types.Link{URL: "https://example.com/gone-page", Type: types.LinkTypeAbsolute},
		Valid:      false,
		Reason:     types.ReasonHTTPError,
		StatusCode: 404,
	}

	// With Wayback enabled but no AI key, EnableAI=false — no fallback since AI not called.
	res := Resolve([]types.ValidationResult{result}, ResolveOptions{
		EnableWayback: true,
		EnableAI:      false,
	})
	if len(res) != 1 {
		t.Fatalf("expected 1 result")
	}
	// No AI key → EXTERNAL_NO_AI reason (Wayback enrichment only runs when AI is also enabled).
	if res[0].UnresolvedReason != types.UnresolvedExternalNoAI {
		t.Errorf("UnresolvedReason = %q, want EXTERNAL_NO_AI", res[0].UnresolvedReason)
	}
}

func TestResolve_SkipsAIForTransientAndBotBlocked(t *testing.T) {
	// AI endpoint that fails the test if it is ever called.
	aiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("AI must not be called for transient/bot-blocked results")
		w.WriteHeader(500)
	}))
	defer aiSrv.Close()
	cfg := AIConfig{APIKey: "k", Model: "m", BaseURL: aiSrv.URL}

	cases := []struct {
		name string
		r    types.ValidationResult
	}{
		{"timeout", types.ValidationResult{Link: types.Link{URL: "https://slow.example.com/", Type: types.LinkTypeAbsolute}, Reason: types.ReasonTimeout}},
		{"418 teapot", types.ValidationResult{Link: types.Link{URL: "https://bot.example.com/", Type: types.LinkTypeAbsolute}, Reason: types.ReasonHTTPError, StatusCode: 418}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := Resolve([]types.ValidationResult{tc.r}, ResolveOptions{EnableAI: true, AI: cfg})
			if out[0].FixedURL != "" {
				t.Errorf("expected no fix, got %q", out[0].FixedURL)
			}
			if out[0].UnresolvedReason != types.UnresolvedBotBlocked {
				t.Errorf("UnresolvedReason = %q, want BOT_BLOCKED", out[0].UnresolvedReason)
			}
		})
	}
}

func TestResolve_URLResolutionReuse(t *testing.T) {
	url := "https://example.com/broken-page"
	results := []types.ValidationResult{
		{Link: types.Link{URL: url, Type: types.LinkTypeAbsolute, SourceFile: "file-a.md"}, Valid: false, Reason: types.ReasonHTTPError, StatusCode: 404},
		{Link: types.Link{URL: url, Type: types.LinkTypeAbsolute, SourceFile: "file-b.md"}, Valid: false, Reason: types.ReasonHTTPError, StatusCode: 404},
	}
	out := Resolve(results, ResolveOptions{})
	if len(out) != 2 {
		t.Fatalf("expected 2 results, got %d", len(out))
	}
	if out[0].ValidationResult.Link.SourceFile != "file-a.md" {
		t.Errorf("result[0] SourceFile = %q, want file-a.md", out[0].ValidationResult.Link.SourceFile)
	}
	if out[1].ValidationResult.Link.SourceFile != "file-b.md" {
		t.Errorf("result[1] SourceFile = %q, want file-b.md", out[1].ValidationResult.Link.SourceFile)
	}
	if out[0].FixedURL != out[1].FixedURL {
		t.Errorf("expected same FixedURL for both, got %q and %q", out[0].FixedURL, out[1].FixedURL)
	}
}
