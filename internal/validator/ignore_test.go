package validator

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		url      string
		patterns []string
		want     bool
	}{
		{"https://internal.example.com/page", []string{"https://internal.*"}, true},
		{"https://github.com/org/repo", []string{"https://internal.*"}, false},
		{"https://a.com", []string{"https://b.*", "https://a.*"}, true},  // second matches
		{"https://a.com", []string{}, false},                              // empty list
		{"https://a.com", nil, false},                                     // nil list
		{"https://exact.com", []string{"https://exact.com"}, true},        // exact match
	}
	for _, tc := range tests {
		got := MatchesAnyPattern(tc.url, tc.patterns)
		if got != tc.want {
			t.Errorf("MatchesAnyPattern(%q, %v) = %v, want %v", tc.url, tc.patterns, got, tc.want)
		}
	}
}

func TestLoadScopedIgnoreFile_Sources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".linkignore")
	content := `# global URL patterns
mailto:*

[/abs/repo/gardener]
/dev-setup/*

[sources]
# root-relative source folders/files to skip entirely
hugo/content/blog/*
hugo/content/community/*
hugo/content/skipme.md
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	si, err := LoadScopedIgnoreFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(si.Global, []string{"mailto:*"}) {
		t.Errorf("Global = %v, want [mailto:*]", si.Global)
	}
	if got := si.RepoPatterns[filepath.Clean("/abs/repo/gardener")]; !reflect.DeepEqual(got, []string{"/dev-setup/*"}) {
		t.Errorf("RepoPatterns = %v, want [/dev-setup/*]", got)
	}
	wantSrc := []string{"hugo/content/blog/*", "hugo/content/community/*", "hugo/content/skipme.md"}
	if !reflect.DeepEqual(si.SourcePatterns, wantSrc) {
		t.Errorf("SourcePatterns = %v, want %v", si.SourcePatterns, wantSrc)
	}
}

func TestLoadScopedIgnoreFile_NoSourcesSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".linkignore")
	if err := os.WriteFile(path, []byte("mailto:*\n\n[/abs/repo]\n/x/*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	si, err := LoadScopedIgnoreFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if si.SourcePatterns != nil {
		t.Errorf("SourcePatterns = %v, want nil when no [sources] section", si.SourcePatterns)
	}
}

func TestLoadScopedIgnoreFile_RepoSourcePrefix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".linkignore")
	content := `[/abs/kubernetes/documentation]
/website/documentation/landscapes/*
source: hugo/content/community/reviews/*
Source: hugo/content/community/mail/*
source:hugo/content/community/community-calls/*
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	si, err := LoadScopedIgnoreFile(path)
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Clean("/abs/kubernetes/documentation")

	// Bare lines → URL-ignore patterns.
	if got := si.RepoPatterns[repo]; !reflect.DeepEqual(got, []string{"/website/documentation/landscapes/*"}) {
		t.Errorf("RepoPatterns = %v, want the single URL pattern", got)
	}
	// source: lines (any case, with/without space) → repo source-skip globs, prefix stripped.
	wantSrc := []string{
		"hugo/content/community/reviews/*",
		"hugo/content/community/mail/*",
		"hugo/content/community/community-calls/*",
	}
	if got := si.RepoSourcePatterns[repo]; !reflect.DeepEqual(got, wantSrc) {
		t.Errorf("RepoSourcePatterns = %v, want %v", got, wantSrc)
	}
}

func TestSourceSkipGlobsFor(t *testing.T) {
	si := ScopedIgnoreFile{
		SourcePatterns: []string{"global/skip/*"},
		RepoSourcePatterns: map[string][]string{
			filepath.Clean("/abs/kube/documentation"): {
				"hugo/content/community/reviews/*",
				"hugo/static/*",
			},
		},
	}

	tests := []struct {
		name string
		root string
		want []string
	}{
		{
			name: "root == repo → globs verbatim + global",
			root: "/abs/kube/documentation",
			want: []string{"global/skip/*", "hugo/content/community/reviews/*", "hugo/static/*"},
		},
		{
			name: "root deeper than repo → in-subtree rebased, out-of-subtree dropped",
			root: "/abs/kube/documentation/hugo/content",
			want: []string{"global/skip/*", "community/reviews/*"}, // hugo/static/* dropped
		},
		{
			name: "repo deeper than root → globs prefixed with rel(root,repo)",
			root: "/abs/kube",
			want: []string{
				"global/skip/*",
				"documentation/hugo/content/community/reviews/*",
				"documentation/hugo/static/*",
			},
		},
		{
			name: "unrelated root → only global",
			root: "/abs/other/repo",
			want: []string{"global/skip/*"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := si.SourceSkipGlobsFor(tc.root)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SourceSkipGlobsFor(%q) =\n  %v\nwant\n  %v", tc.root, got, tc.want)
			}
		})
	}
}
