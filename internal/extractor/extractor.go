package extractor

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// deduplicateLinks removes duplicate links with the same (SourceFile, URL, Type),
// keeping the first occurrence (lowest Start offset). This prevents the same URL
// being validated and resolved multiple times from the same file (e.g. badge images
// that appear as both an img src and an anchor href).
func deduplicateLinks(links []types.Link) []types.Link {
	seen := make(map[string]bool, len(links))
	out := make([]types.Link, 0, len(links))
	for _, l := range links {
		key := l.SourceFile + "\x00" + l.URL + "\x00" + string(l.Type)
		if !seen[key] {
			seen[key] = true
			out = append(out, l)
		}
	}
	return out
}

// ExtractFile extracts links from a single file, dispatching by extension.
// Returns an error if the file cannot be read or has an unsupported extension.
func ExtractFile(path string) ([]types.Link, error) {
	var links []types.Link
	var err error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md":
		links, err = ExtractMarkdown(path)
	case ".html":
		links, err = ExtractHTML(path)
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return deduplicateLinks(links), nil
}

// Extract collects all .md and .html files under root (optionally scoped to dirs,
// and excluding sourceIgnore globs) and returns every link found across all files.
func Extract(root string, dirs, sourceIgnore []string) ([]types.Link, error) {
	files, err := Collect(root, dirs, sourceIgnore)
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
		all = append(all, deduplicateLinks(links)...)
	}
	return all, nil
}
