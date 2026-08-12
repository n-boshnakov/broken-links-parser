package extractor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestSplitURLTitle(t *testing.T) {
	cases := []struct {
		raw     string
		wantURL string
		wantTrm int
	}{
		{`/path/img.svg`, `/path/img.svg`, 0},
		{`/path/img.svg "A title"`, `/path/img.svg`, len(` "A title"`)},
		{`/path/img.svg 'single'`, `/path/img.svg`, len(` 'single'`)},
		{`/path/img.svg (paren title)`, `/path/img.svg`, len(` (paren title)`)},
		{`https://x.com/p "t"`, `https://x.com/p`, len(` "t"`)},
		{`/trailing/space `, `/trailing/space`, 1},
	}
	for _, c := range cases {
		gotURL, gotTrm := splitURLTitle(c.raw)
		if gotURL != c.wantURL || gotTrm != c.wantTrm {
			t.Errorf("splitURLTitle(%q) = (%q, %d), want (%q, %d)", c.raw, gotURL, gotTrm, c.wantURL, c.wantTrm)
		}
	}
}

func TestExtractMarkdown_StripsLinkTitles(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "titled.md")
	content := "![alt](/docs/img.svg \"Diagram Title\")\n\n" +
		"[link](/docs/page \"Page Title\")\n\n" +
		"[ref-style][r]\n\n[r]: /docs/other \"Ref Title\"\n"
	_ = os.WriteFile(f, []byte(content), 0o644)

	links, err := ExtractMarkdown(f)
	if err != nil {
		t.Fatal(err)
	}
	byURL := map[string]types.Link{}
	for _, l := range links {
		byURL[l.URL] = l
	}

	// Titles must be stripped from all destinations.
	for _, want := range []string{"/docs/img.svg", "/docs/page", "/docs/other"} {
		if _, ok := byURL[want]; !ok {
			t.Errorf("expected clean URL %q; got links: %v", want, keys(byURL))
		}
	}

	// Offsets must still point exactly at the (trimmed) URL bytes.
	data, _ := os.ReadFile(f)
	for _, l := range links {
		if l.Start < 0 || l.End > len(data) || l.Start >= l.End {
			t.Errorf("link %q invalid offsets [%d,%d)", l.URL, l.Start, l.End)
			continue
		}
		if got := string(data[l.Start:l.End]); got != l.URL {
			t.Errorf("link %q: bytes at [%d,%d) = %q", l.URL, l.Start, l.End, got)
		}
	}
}

func keys(m map[string]types.Link) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExtractMarkdown(t *testing.T) {
	links, err := ExtractMarkdown("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}

	type want struct {
		url      string
		linkType types.LinkType
	}

	// Build a lookup: url → linkType from the extracted slice.
	got := map[string]types.LinkType{}
	for _, l := range links {
		got[l.URL] = l.Type
	}

	expects := []want{
		{"../guide/intro.md", types.LinkTypeRelative},
		{"https://github.com/org/repo", types.LinkTypeAbsolute},
		{"./images/arch.png", types.LinkTypeImage},
		{"#installation", types.LinkTypeAnchor},
		{"https://example.com/guide", types.LinkTypeAbsolute}, // resolved ref
	}

	for _, e := range expects {
		got, ok := got[e.url]
		if !ok {
			t.Errorf("expected link %q not found", e.url)
			continue
		}
		if got != e.linkType {
			t.Errorf("link %q: got type %q, want %q", e.url, got, e.linkType)
		}
	}

	// Items that must NOT appear (inside code blocks / undefined refs).
	forbidden := []string{
		"http://should-not-appear.com",
		"http://not-this.com",
	}
	gotMap := map[string]bool{}
	for _, l := range links {
		gotMap[l.URL] = true
	}
	for _, u := range forbidden {
		if gotMap[u] {
			t.Errorf("link %q should have been excluded (inside code region)", u)
		}
	}

	// Verify byte offsets are consistent with the actual URL bytes in the file.
	data, _ := os.ReadFile("testdata/sample.md")
	for _, l := range links {
		if l.Start < 0 || l.End > len(data) || l.Start >= l.End {
			t.Errorf("link %q has invalid offsets [%d, %d)", l.URL, l.Start, l.End)
			continue
		}
		if got := string(data[l.Start:l.End]); got != l.URL {
			t.Errorf("link %q: bytes at [%d,%d) = %q", l.URL, l.Start, l.End, got)
		}
	}
}
