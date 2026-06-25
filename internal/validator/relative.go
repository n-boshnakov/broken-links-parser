package validator

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

var (
	reMDHeading   = regexp.MustCompile(`^#{1,6}\s+(.+)`)
	reHTMLHeading = regexp.MustCompile(`(?i)<h[1-6][^>]*>([^<]+)</h[1-6]>`)
	reNonAlnum    = regexp.MustCompile(`[^\p{L}\p{N}\- ]`)
)

// NormaliseAnchor converts a heading string to its GitHub-flavored anchor form:
// lowercase, spaces→hyphens, strip characters that are not letters, digits, or hyphens.
func NormaliseAnchor(heading string) string {
	s := strings.ToLower(heading)
	s = reNonAlnum.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, " ", "-")
	// Collapse multiple hyphens that may result from stripping.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// ExtractAnchors returns the normalised anchor IDs of all headings in path.
// Supports Markdown (# headings) and HTML (<h1>–<h6> tags).
func ExtractAnchors(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var anchors []string
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".md" {
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			if m := reMDHeading.FindStringSubmatch(scanner.Text()); m != nil {
				anchors = append(anchors, NormaliseAnchor(m[1]))
			}
		}
	}

	// HTML headings — works for both .html files and as a fallback pass on .md
	// (some Markdown files embed raw HTML headings).
	for _, m := range reHTMLHeading.FindAllStringSubmatch(content, -1) {
		anchors = append(anchors, NormaliseAnchor(m[1]))
	}

	return anchors, nil
}

// ValidateRelative checks a relative or anchor-only link against the local filesystem.
func ValidateRelative(link types.Link, patterns []string) types.ValidationResult {
	if MatchesAnyPattern(link.URL, patterns) {
		return types.ValidationResult{Link: link, Valid: true, Reason: types.ReasonIgnored}
	}

	url := link.URL
	fragment := ""
	if i := strings.Index(url, "#"); i >= 0 {
		fragment = url[i+1:]
		url = url[:i]
	}

	// Anchor-only link — target is the source file itself.
	targetPath := link.SourceFile
	if url != "" {
		targetPath = filepath.Join(filepath.Dir(link.SourceFile), filepath.FromSlash(url))
	}

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonFileNotFound}
	}

	if fragment != "" {
		anchors, err := ExtractAnchors(targetPath)
		if err != nil {
			return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonFileNotFound}
		}
		for _, a := range anchors {
			if a == fragment {
				return types.ValidationResult{Link: link, Valid: true}
			}
		}
		return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonAnchorNotFound}
	}

	return types.ValidationResult{Link: link, Valid: true}
}
