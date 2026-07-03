package validator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestNormaliseAnchor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Installation", "installation"},
		{"Getting Started", "getting-started"},
		{"foo & bar", "foo--bar"}, // & stripped, surrounding spaces each become -, giving --
		{"  spaces  ", "spaces"},
	}
	for _, c := range cases {
		if got := NormaliseAnchor(c.in); got != c.want {
			t.Errorf("NormaliseAnchor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateRelative(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) string {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return p
	}

	srcFile := write("docs/guide.md", "# Installation\n\nSome text.\n")
	write("docs/other.md", "# Overview\n")

	link := func(url string, lt types.LinkType) types.Link {
		return types.Link{URL: url, Type: lt, SourceFile: srcFile}
	}

	tests := []struct {
		name     string
		link     types.Link
		patterns []string
		wantOK   bool
		wantReason string
	}{
		{"valid relative file", link("other.md", types.LinkTypeRelative), nil, true, ""},
		{"missing file", link("missing.md", types.LinkTypeRelative), nil, false, types.ReasonFileNotFound},
		{"valid anchor", link("guide.md#installation", types.LinkTypeRelative), nil, true, ""},
		{"missing anchor", link("guide.md#no-such-heading", types.LinkTypeRelative), nil, false, types.ReasonAnchorNotFound},
		{"same-file anchor valid", link("#installation", types.LinkTypeAnchor), nil, true, ""},
		{"same-file anchor missing", link("#nope", types.LinkTypeAnchor), nil, false, types.ReasonAnchorNotFound},
		{"ignored by pattern", link("missing.md", types.LinkTypeRelative), []string{"missing*"}, true, types.ReasonIgnored},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateRelative(tc.link, tc.patterns, "")
			if got.Valid != tc.wantOK {
				t.Errorf("Valid: got %v, want %v", got.Valid, tc.wantOK)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason: got %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}
