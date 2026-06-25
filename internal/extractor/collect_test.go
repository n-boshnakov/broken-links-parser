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
			got, err := Collect(root, tc.dirs)
			if err != nil {
				t.Fatal(err)
			}
			if g, w := sorted(got), tc.want; !equalSlices(g, w) {
				t.Errorf("got %v, want %v", g, w)
			}
		})
	}

	t.Run("symlink dir not followed", func(t *testing.T) {
		got, err := Collect(root, nil)
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
