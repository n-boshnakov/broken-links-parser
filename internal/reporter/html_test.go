package reporter

import "testing"

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
