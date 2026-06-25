package extractor

import (
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestExtractHTML(t *testing.T) {
	links, err := ExtractHTML("testdata/sample.html")
	if err != nil {
		t.Fatal(err)
	}

	type want struct {
		url      string
		linkType types.LinkType
	}

	expects := []want{
		{"../other/page.html", types.LinkTypeRelative},
		{"https://github.com/org/repo", types.LinkTypeAbsolute},
		{"#section", types.LinkTypeAnchor},
		{"./images/logo.png", types.LinkTypeImage},
		{"https://cdn.example.com/banner.png", types.LinkTypeImage}, // <img> is always image
		{"https://example.com/after-malformed", types.LinkTypeAbsolute},
	}

	got := map[string]types.LinkType{}
	for _, l := range links {
		got[l.URL] = l.Type
	}

	for _, e := range expects {
		typ, ok := got[e.url]
		if !ok {
			t.Errorf("expected link %q not found", e.url)
			continue
		}
		if typ != e.linkType {
			t.Errorf("link %q: got type %q, want %q", e.url, typ, e.linkType)
		}
	}

	// Empty href must not appear.
	if _, ok := got[""]; ok {
		t.Error("empty href should not be extracted")
	}
}
