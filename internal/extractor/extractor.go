package extractor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// Extract collects all .md and .html files under root (optionally scoped to dirs)
// and returns every link found across all files.
func Extract(root string, dirs []string) ([]types.Link, error) {
	files, err := Collect(root, dirs)
	if err != nil {
		return nil, fmt.Errorf("collect: %w", err)
	}

	var all []types.Link
	for _, f := range files {
		var links []types.Link
		switch strings.ToLower(filepath.Ext(f)) {
		case ".md":
			links, err = ExtractMarkdown(f)
		case ".html":
			links, err = ExtractHTML(f)
		}
		if err != nil {
			return nil, fmt.Errorf("extract %s: %w", f, err)
		}
		all = append(all, links...)
	}
	return all, nil
}
