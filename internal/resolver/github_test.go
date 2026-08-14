package resolver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestFindLocalClone(t *testing.T) {
	dir := t.TempDir()
	// Create a fake git repo at dir/gardener/documentation
	repoPath := filepath.Join(dir, "gardener", "documentation")
	_ = os.MkdirAll(filepath.Join(repoPath, ".git"), 0o755)

	if p, ok := findLocalClone(dir, "", "gardener", "documentation", true, nil); !ok || p != repoPath {
		t.Errorf("findLocalClone(dir, gardener, documentation) = %q, %v; want %q, true", p, ok, repoPath)
	}
	// Flat layout: dir/documentation
	dir2 := t.TempDir()
	repoPath2 := filepath.Join(dir2, "documentation")
	_ = os.MkdirAll(filepath.Join(repoPath2, ".git"), 0o755)
	if p, ok := findLocalClone(dir2, "", "gardener", "documentation", true, nil); !ok || p != repoPath2 {
		t.Errorf("flat layout: findLocalClone = %q, %v; want %q, true", p, ok, repoPath2)
	}
	// Not found
	if _, ok := findLocalClone(t.TempDir(), "", "org", "missing", true, nil); ok {
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

func TestResolveViaGitHubAPI_UsesBranch(t *testing.T) {
	var gotRefPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRefPath = r.URL.Path
		resp := map[string]interface{}{
			"tree": []map[string]string{{"path": "docs/logging.md", "type": "blob"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	result := types.ValidationResult{
		Link:  types.Link{URL: "https://github.com/o/r/blob/release-1.2/docs/old/logging.md", Type: types.LinkTypeAbsolute},
		Valid: false, Reason: types.ReasonHTTPError,
	}
	res := resolveViaGitHubAPIWithBase(result, "", srv.URL)
	if res.FixedURL == "" {
		t.Fatal("expected FixedURL")
	}
	// The tree request must target the URL's branch, not HEAD.
	if !strings.Contains(gotRefPath, "/git/trees/release-1.2") {
		t.Errorf("tree request path = %q, want it to contain the branch 'release-1.2'", gotRefPath)
	}
}

func TestResolveViaGitHubAPI_BranchFallbackToHEAD(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/git/trees/gone-branch") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// HEAD tree contains the file.
		resp := map[string]interface{}{
			"tree": []map[string]string{{"path": "docs/logging.md", "type": "blob"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	result := types.ValidationResult{
		Link:  types.Link{URL: "https://github.com/o/r/blob/gone-branch/docs/old/logging.md", Type: types.LinkTypeAbsolute},
		Valid: false, Reason: types.ReasonHTTPError,
	}
	res := resolveViaGitHubAPIWithBase(result, "", srv.URL)
	if res.FixedURL == "" {
		t.Fatalf("expected FixedURL via HEAD fallback, got unresolved (%s)", res.UnresolvedReason)
	}
}

func TestResolveViaGitHubAPI_SHAPinnedResolvesAtHEAD(t *testing.T) {
	sha := "b3a501cbe11e46bea1f8879d39c8abcdef012345" // exactly 40 hex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// At HEAD the file lives at a new path (moved since the pinned commit).
		resp := map[string]interface{}{
			"tree": []map[string]string{{"path": "docs/new/logging.md", "type": "blob"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	result := types.ValidationResult{
		Link:  types.Link{URL: "https://github.com/o/r/blob/" + sha + "/docs/old/logging.md", Type: types.LinkTypeAbsolute},
		Valid: false, Reason: types.ReasonHTTPError,
	}
	res := resolveViaGitHubAPIWithBase(result, "", srv.URL)
	if res.FixedURL == "" {
		t.Fatalf("expected FixedURL for SHA-pinned resolved at HEAD, got unresolved (%s)", res.UnresolvedReason)
	}
	// Must rebuild against HEAD, not the stale SHA, and at reduced confidence.
	if strings.Contains(res.FixedURL, sha) {
		t.Errorf("FixedURL should not contain the pinned SHA, got %q", res.FixedURL)
	}
	if res.ConfidenceScore > 0.6 {
		t.Errorf("SHA-pinned confidence = %v, want <= 0.6", res.ConfidenceScore)
	}
}

func TestFollowRenameChain(t *testing.T) {
	// commits?path=b.md → a commit sha; commits/<sha> → renamed b.md → c.md.
	// c.md is present in the tree index, so the chain terminates there.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/commits/") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"files": []map[string]string{
					{"status": "renamed", "previous_filename": "b.md", "filename": "c.md"},
				},
			})
			return
		}
		if strings.Contains(r.URL.Path, "/commits") {
			_ = json.NewEncoder(w).Encode([]map[string]string{{"sha": "deadbeef"}})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	index := map[string]string{"c.md": "docs/c.md"} // terminal path present at HEAD
	got := followRenameChain(srv.Client(), srv.URL, "o", "r", index, "b.md", "")
	if got != "c.md" {
		t.Errorf("followRenameChain = %q, want c.md (terminal path)", got)
	}
}

func TestAPIBaseForHost(t *testing.T) {
	if got := apiBaseForHost("github.com"); got != "https://api.github.com" {
		t.Errorf("github.com: got %q", got)
	}
	if got := apiBaseForHost("github.tools.sap"); got != "https://github.tools.sap/api/v3" {
		t.Errorf("github.tools.sap: got %q", got)
	}
	if got := apiBaseForHost("github.wdf.sap.corp"); got != "https://github.wdf.sap.corp/api/v3" {
		t.Errorf("github.wdf.sap.corp: got %q", got)
	}
}

func TestRebuildGitHubURL(t *testing.T) {
	tests := []struct {
		name     string
		original string
		newPath  string
		want     string
	}{
		{
			name:     "github.com preserves host",
			original: "https://github.com/gardener/gardener/blob/master/docs/old/logging.md",
			newPath:  "docs/new/logging.md",
			want:     "https://github.com/gardener/gardener/blob/master/docs/new/logging.md",
		},
		{
			name:     "enterprise host is preserved",
			original: "https://github.tools.sap/kubernetes/docs/blob/main/setup/install.md",
			newPath:  "setup/getting-started/install.md",
			want:     "https://github.tools.sap/kubernetes/docs/blob/main/setup/getting-started/install.md",
		},
		{
			name:     "fragment is carried over",
			original: "https://github.tools.sap/org/repo/blob/main/a/b.md#section",
			newPath:  "c/d.md",
			want:     "https://github.tools.sap/org/repo/blob/main/c/d.md#section",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rebuildGitHubURL(tt.original, tt.newPath); got != tt.want {
				t.Errorf("rebuildGitHubURL() = %q, want %q", got, tt.want)
			}
		})
	}
}


func TestResolveViaGitHubAPI_NonFileURLs(t *testing.T) {
	// These GitHub URLs are not /blob/<branch>/<file> links, so they can't be
	// resolved via file history and must report UNSUPPORTED_GITHUB_URL (not NO_HISTORY).
	cases := []string{
		"https://github.com/gardener/gardener/tree/master/docs/proposals",       // directory
		"https://github.com/gardener/backlog/issues/29",                          // issue
		"https://github.com/gardener/golangci-logcheck",                          // bare repo
		"https://github.com/rgroemmer",                                           // user profile
	}
	for _, url := range cases {
		t.Run(url, func(t *testing.T) {
			result := types.ValidationResult{
				Link:  types.Link{URL: url, Type: types.LinkTypeAbsolute},
				Valid: false, Reason: types.ReasonHTTPError, StatusCode: 404,
			}
			// apiBase is unused because parseGitHubURL fails before any request.
			res := resolveViaGitHubAPIWithBase(result, "", "https://api.github.com")
			if res.FixedURL != "" {
				t.Errorf("expected no fix, got %q", res.FixedURL)
			}
			if res.UnresolvedReason != types.UnresolvedUnsupportedGitHubURL {
				t.Errorf("UnresolvedReason = %q, want UNSUPPORTED_GITHUB_URL", res.UnresolvedReason)
			}
		})
	}
}

func TestResolveViaGitHubAPI_TreeFileLinkResolves(t *testing.T) {
	// A file mistyped with /tree/ instead of /blob/ (path ends in .md) must be
	// treated as a file link and resolved, with the fix rebuilt as a /blob/ URL.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"tree": []map[string]string{{"path": "docs/proposals/28-autonomous-shoot-clusters.md", "type": "blob"}},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	result := types.ValidationResult{
		Link: types.Link{
			// mistyped /tree/ for a file, and at a stale path so the tree lookup relocates it
			URL:  "https://github.com/gardener/gardener/tree/master/docs/old/28-autonomous-shoot-clusters.md",
			Type: types.LinkTypeAbsolute,
		},
		Valid: false, Reason: types.ReasonHTTPError, StatusCode: 404,
	}
	res := resolveViaGitHubAPIWithBase(result, "", srv.URL)
	if res.FixedURL == "" {
		t.Fatalf("expected a fix for a /tree/ file link, got unresolved (%s)", res.UnresolvedReason)
	}
	if !strings.Contains(res.FixedURL, "/blob/") {
		t.Errorf("fix should be rebuilt as a /blob/ URL, got %q", res.FixedURL)
	}
}
