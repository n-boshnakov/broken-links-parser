package extractor

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestExtract_Integration(t *testing.T) {
	root := t.TempDir()

	writeFile := func(rel, content string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(content), 0o644)
	}

	writeFile("docs/guide.md", "[intro](../intro.md)\n[external](https://example.com)\n")
	writeFile("web/index.html", `<a href="./page.html">page</a><img src="./logo.png">`)

	links, err := Extract(root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	byURL := map[string]types.LinkType{}
	for _, l := range links {
		byURL[l.URL] = l.Type
	}

	checks := []struct {
		url      string
		wantType types.LinkType
	}{
		{"../intro.md", types.LinkTypeRelative},
		{"https://example.com", types.LinkTypeAbsolute},
		{"./page.html", types.LinkTypeRelative},
		{"./logo.png", types.LinkTypeImage},
	}

	for _, c := range checks {
		got, ok := byURL[c.url]
		if !ok {
			t.Errorf("link %q not found", c.url)
			continue
		}
		if got != c.wantType {
			t.Errorf("link %q: type %q, want %q", c.url, got, c.wantType)
		}
	}
}
