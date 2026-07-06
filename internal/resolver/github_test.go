package resolver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestFindLocalClone(t *testing.T) {
	dir := t.TempDir()
	// Create a fake git repo at dir/gardener/documentation
	repoPath := filepath.Join(dir, "gardener", "documentation")
	_ = os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755)

	if p, ok := findLocalClone(dir, "", "gardener", "documentation", true); !ok || p != repoPath {
		t.Errorf("findLocalClone(dir, gardener, documentation) = %q, %v; want %q, true", p, ok, repoPath)
	}
	// Flat layout: dir/documentation
	dir2 := t.TempDir()
	repoPath2 := filepath.Join(dir2, "documentation")
	_ = os.MkdirAll(filepath.Join(repoPath2, ".git"), 0o755)
	if p, ok := findLocalClone(dir2, "", "gardener", "documentation", true); !ok || p != repoPath2 {
		t.Errorf("flat layout: findLocalClone = %q, %v; want %q, true", p, ok, repoPath2)
	}
	// Not found
	if _, ok := findLocalClone(t.TempDir(), "", "org", "missing", true); ok {
		t.Error("expected not found for missing repo")
	}
}

func TestBuildFileIndex(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"tree": []map[string]string{
				{"path": "docs/guide.md", "type": "blob"},
				{"path": "website/index.html", "type": "blob"},
				{"path": "docs", "type": "tree"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	// Patch the API URL by making buildFileIndex accept a base URL in tests.
	// For simplicity, test the index logic directly via a helper.
	index, err := buildFileIndexFromURL(srv.Client(), srv.URL+"/repos/org/repo/git/trees/HEAD?recursive=1", "")
	if err != nil {
		t.Fatal(err)
	}
	if index["guide.md"] != "docs/guide.md" {
		t.Errorf("guide.md → %q, want docs/guide.md", index["guide.md"])
	}
	if index["index.html"] != "website/index.html" {
		t.Errorf("index.html → %q, want website/index.html", index["index.html"])
	}
	if _, ok := index["docs"]; ok {
		t.Error("tree entries should not be in the index")
	}
}

func TestResolveViaGitHubAPI_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"tree": []map[string]string{
				{"path": "new/path/logging.md", "type": "blob"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	result := types.ValidationResult{
		Link: types.Link{
			URL:  "https://github.com/gardener/gardener/blob/master/docs/old/logging.md",
			Type: types.LinkTypeAbsolute,
		},
		Valid:  false,
		Reason: types.ReasonHTTPError,
	}

	res := resolveViaGitHubAPIWithBase(result, "", srv.URL)
	if res.FixedURL == "" {
		t.Fatal("expected FixedURL")
	}
	if res.Strategy != types.StrategyGitHubAPI {
		t.Errorf("Strategy = %q", res.Strategy)
	}
}
