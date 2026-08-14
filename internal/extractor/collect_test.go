package extractor

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestCollect(t *testing.T) {
	// Build a temp tree:
	//   root/
	//     a.md
	//     b.html
	//     c.txt          (ignored)
	//     docs/
	//       d.md
	//     website/
	//       e.html
	//     link -> docs/  (symlink dir, not followed)
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(""), 0o644)
	}
	write("a.md")
	write("b.html")
	write("c.txt")
	write("docs/d.md")
	write("website/e.html")
	_ = os.Symlink(filepath.Join(root, "docs"), filepath.Join(root, "link"))

	sorted := func(paths []string) []string {
		rel := make([]string, len(paths))
		for i, p := range paths {
			r, _ := filepath.Rel(root, p)
			rel[i] = r
		}
		sort.Strings(rel)
		return rel
	}

	tests := []struct {
		name string
		dirs []string
		want []string
	}{
		{
			name: "full scan",
			dirs: nil,
			want: []string{"a.md", "b.html", "docs/d.md", "website/e.html"},
		},
		{
			name: "empty dirs treated as full scan",
			dirs: []string{},
			want: []string{"a.md", "b.html", "docs/d.md", "website/e.html"},
		},
		{
			name: "scoped to docs",
			dirs: []string{"docs"},
			want: []string{"docs/d.md"},
		},
		{
			name: "scoped to multiple dirs",
			dirs: []string{"docs", "website"},
			want: []string{"docs/d.md", "website/e.html"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Collect(root, tc.dirs, nil)
			if err != nil {
				t.Fatal(err)
			}
			if g, w := sorted(got), tc.want; !equalSlices(g, w) {
				t.Errorf("got %v, want %v", g, w)
			}
		})
	}

	t.Run("symlink dir not followed", func(t *testing.T) {
		got, err := Collect(root, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range got {
			r, _ := filepath.Rel(root, p)
			if len(r) > 4 && r[:4] == "link" {
				t.Errorf("symlinked path included: %s", r)
			}
		}
	})
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCollect_SkipsNonContentDirs(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(""), 0o644)
	}
	// Real content.
	write("content/real.md")
	write("guide.md")
	// Junk that must be skipped.
	write("node_modules/pkg/readme.md")
	write(".git/HEAD.md")
	write(".vitepress/cache/x.md")
	write("vendor/dep/doc.html")
	write("dist/bundle.html")
	write("build/out.md")
	write("public/index.html")
	write("docs/.hidden/secret.md") // hidden nested dir

	got, err := Collect(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rel := make([]string, len(got))
	for i, p := range got {
		r, _ := filepath.Rel(root, p)
		rel[i] = r
	}
	sort.Strings(rel)

	want := []string{"content/real.md", "guide.md"}
	if !equalSlices(rel, want) {
		t.Errorf("got %v, want %v", rel, want)
	}
}

func TestCollect_SourceIgnore(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(""), 0o644)
	}
	write("hugo/content/docs/keep.md")
	write("hugo/content/blog/2025/07/post.md")     // under an ignored folder
	write("hugo/content/blog/2024/old.md")         // under an ignored folder
	write("hugo/content/community/notes.md")       // ignored folder
	write("hugo/content/skipme.md")                // ignored single file

	ignore := []string{
		"hugo/content/blog/*",
		"hugo/content/community/*",
		"hugo/content/skipme.md",
	}
	got, err := Collect(root, nil, ignore)
	if err != nil {
		t.Fatal(err)
	}
	rel := make([]string, len(got))
	for i, p := range got {
		r, _ := filepath.Rel(root, p)
		rel[i] = filepath.ToSlash(r)
	}
	sort.Strings(rel)

	want := []string{"hugo/content/docs/keep.md"}
	if !equalSlices(rel, want) {
		t.Errorf("got %v, want %v", rel, want)
	}
}
