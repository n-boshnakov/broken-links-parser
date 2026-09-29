package reporter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/pipeline"
	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// extractRow returns the <tr>…</tr> substring whose URL cell contains urlNeedle.
// It lets the assertions target one row's data-fixed without matching others.
func extractRow(t *testing.T, html, urlNeedle string) string {
	t.Helper()
	idx := strings.Index(html, urlNeedle)
	if idx < 0 {
		t.Fatalf("URL %q not found in report", urlNeedle)
	}
	start := strings.LastIndex(html[:idx], "<tr")
	end := strings.Index(html[idx:], "</tr>")
	if start < 0 || end < 0 {
		t.Fatalf("could not bound the <tr> for %q", urlNeedle)
	}
	return html[start : idx+end]
}

// TestWriteHTML_AnchorFixGate is the direct regression guard for the missing
// confidence gate: a high-scoring anchor suggestion renders as a confident fix
// (data-fixed="yes"), a low-scoring one as a clickable "Possible match" hint
// (data-fixed="hint"), and a suggestion-less broken link as data-fixed="no".
func TestWriteHTML_AnchorFixGate(t *testing.T) {
	mkLink := func(url string) types.Link {
		return types.Link{URL: url, Type: types.LinkTypeRelative, SourceFile: "/repo/src.md"}
	}
	res := &pipeline.Result{
		Links: []types.Link{
			mkLink("guide.md#installation-steps"), // strong suggestion → yes
			mkLink("guide.md#backupbucketconfig"), // weak suggestion → hint
			mkLink("guide.md#totally-unrelated"),  // no suggestion → no
		},
		Validations: []types.ValidationResult{
			{
				Link:                 mkLink("guide.md#installation-steps"),
				Valid:                false,
				Reason:               types.ReasonAnchorNotFound,
				SuggestedAnchor:      "installation-step",
				SuggestedAnchorScore: 0.9,
			},
			{
				Link:                 mkLink("guide.md#backupbucketconfig"),
				Valid:                false,
				Reason:               types.ReasonAnchorNotFound,
				SuggestedAnchor:      "workerconfig",
				SuggestedAnchorScore: 0.5,
			},
			{
				Link:   mkLink("guide.md#totally-unrelated"),
				Valid:  false,
				Reason: types.ReasonAnchorNotFound,
			},
		},
	}

	out := filepath.Join(t.TempDir(), "report.html")
	if err := WriteHTML(out, "/repo", "", res); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	html := string(data)

	cases := []struct {
		urlNeedle string
		wantFixed string
	}{
		{"guide.md#installation-steps", `data-fixed="yes"`},
		{"guide.md#backupbucketconfig", `data-fixed="hint"`},
		{"guide.md#totally-unrelated", `data-fixed="no"`},
	}
	for _, tc := range cases {
		row := extractRow(t, html, tc.urlNeedle)
		if !strings.Contains(row, tc.wantFixed) {
			t.Errorf("row for %q: missing %s\nrow: %s", tc.urlNeedle, tc.wantFixed, row)
		}
	}

	// The hint row must carry the "Possible match" label and strategy, and the report
	// must expose a "Possible match" filter chip.
	hintRow := extractRow(t, html, "guide.md#backupbucketconfig")
	if !strings.Contains(hintRow, "Possible match") {
		t.Errorf("hint row missing 'Possible match' label:\n%s", hintRow)
	}
	if !strings.Contains(html, `toggleChip(this,'fixed','hint')`) {
		t.Error("report missing the 'Possible match' filter chip")
	}
}

func TestResolveGitHubAnchorURL(t *testing.T) {
	const src = "https://github.com/gardener/documentation/blob/master/hugo/content/blog/2025/07/post.md"

	cases := []struct {
		name       string
		sourceURL  string
		relTarget  string
		anchor     string
		contentRel string
		want       string
	}{
		{
			name:       "root-relative resolves against content root, not source dir",
			sourceURL:  src,
			relTarget:  "/docs/extensions/provider-aws/usage/",
			anchor:     "workerconfig",
			contentRel: "hugo/content",
			want:       "https://github.com/gardener/documentation/blob/master/hugo/content/docs/extensions/provider-aws/usage#workerconfig",
		},
		{
			name:       "root-relative with empty contentRel returns empty (safe fallback)",
			sourceURL:  src,
			relTarget:  "/docs/x/",
			anchor:     "y",
			contentRel: "",
			want:       "",
		},
		{
			name:       "relative ../ still resolves against source dir",
			sourceURL:  "https://github.com/o/r/blob/main/docs/a/page.md",
			relTarget:  "../b/other.md",
			anchor:     "sec",
			contentRel: "hugo/content",
			want:       "https://github.com/o/r/blob/main/docs/b/other.md#sec",
		},
		{
			name:       "anchor-only targets the source file",
			sourceURL:  "https://github.com/o/r/blob/main/docs/page.md",
			relTarget:  "",
			anchor:     "installation",
			contentRel: "hugo/content",
			want:       "https://github.com/o/r/blob/main/docs/page.md#installation",
		},
		{
			name:       "non-GitHub source returns empty",
			sourceURL:  "https://example.com/some/page.md",
			relTarget:  "/docs/x/",
			anchor:     "y",
			contentRel: "hugo/content",
			want:       "",
		},
		{
			name:       "root-relative normalises .. and trailing slash",
			sourceURL:  "https://github.com/o/r/blob/main/hugo/content/blog/post.md",
			relTarget:  "/docs/../guides/setup/",
			anchor:     "start",
			contentRel: "hugo/content",
			want:       "https://github.com/o/r/blob/main/hugo/content/guides/setup#start",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveGitHubAnchorURL(tc.sourceURL, tc.relTarget, tc.anchor, tc.contentRel)
			if got != tc.want {
				t.Errorf("resolveGitHubAnchorURL(...) =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

func TestRepoBlobURL(t *testing.T) {
	const src = "https://github.com/gardener/documentation/blob/master/hugo/content/blog/2025/07/post.md"
	cases := []struct {
		name        string
		sourceURL   string
		repoRelPath string
		anchor      string
		want        string
	}{
		{
			name:        "builds URL to the real .md file (not the extensionless route)",
			sourceURL:   src,
			repoRelPath: "hugo/content/docs/extensions/provider-aws/usage.md",
			anchor:      "workerconfig",
			want:        "https://github.com/gardener/documentation/blob/master/hugo/content/docs/extensions/provider-aws/usage.md#workerconfig",
		},
		{
			name:        "no anchor",
			sourceURL:   src,
			repoRelPath: "hugo/content/docs/x.md",
			anchor:      "",
			want:        "https://github.com/gardener/documentation/blob/master/hugo/content/docs/x.md",
		},
		{
			name:        "non-GitHub source returns empty",
			sourceURL:   "https://example.com/x.md",
			repoRelPath: "docs/x.md",
			anchor:      "y",
			want:        "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := repoBlobURL(tc.sourceURL, tc.repoRelPath, tc.anchor); got != tc.want {
				t.Errorf("repoBlobURL(...) =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}
