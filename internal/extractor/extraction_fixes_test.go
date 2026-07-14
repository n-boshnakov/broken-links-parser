package extractor

import (
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestFootnoteNotExtracted(t *testing.T) {
	links, err := ExtractMarkdown("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	byURL := map[string]bool{}
	for _, l := range links {
		byURL[l.URL] = true
	}
	// Footnote text should not be extracted as a link.
	if byURL["OTel"] || byURL["OTel Specification"] {
		t.Error("footnote text extracted as relative link")
	}
	// But the URL inside the footnote should be extracted.
	if !byURL["https://opentelemetry.io/docs/specs/"] {
		t.Error("URL inside footnote not extracted")
	}
}

func TestBalancedParenthesesURL(t *testing.T) {
	links, err := ExtractMarkdown("testdata/sample.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "https://en.wikipedia.org/wiki/Defense_in_depth_(computing)"
	found := false
	for _, l := range links {
		if l.URL == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("URL with balanced parens not extracted: %q", want)
	}
}

func TestDeduplicateLinks(t *testing.T) {
	links := []types.Link{
		{URL: "https://example.com", Type: types.LinkTypeAbsolute, SourceFile: "a.md", Start: 0, End: 20},
		{URL: "https://example.com", Type: types.LinkTypeAbsolute, SourceFile: "a.md", Start: 50, End: 70}, // dup
		{URL: "https://example.com", Type: types.LinkTypeImage, SourceFile: "a.md", Start: 100, End: 120},  // diff type — kept
		{URL: "https://other.com", Type: types.LinkTypeAbsolute, SourceFile: "a.md", Start: 200, End: 215},
	}
	got := deduplicateLinks(links)
	if len(got) != 3 {
		t.Errorf("expected 3 after dedup, got %d", len(got))
	}
	// First occurrence kept.
	if got[0].Start != 0 {
		t.Errorf("expected Start=0 for first occurrence, got %d", got[0].Start)
	}
}
