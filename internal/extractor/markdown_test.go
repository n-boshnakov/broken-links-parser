package extractor

import (
	"os"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

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
