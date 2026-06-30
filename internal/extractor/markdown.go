package extractor

import (
	"os"
	"regexp"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// Compiled once at init time.
var (
	// Fenced code blocks: ```...``` (multiline, non-greedy).
	reFenced = regexp.MustCompile("(?s)```[^`]*?```")
	// Inline code spans: `...` (single backtick, no newlines).
	reInlineCode = regexp.MustCompile("`[^`\n]+`")

	// ![alt](src)
	reImage = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	// Any [text](url) — we filter out images by checking the preceding byte.
	reAnyInline = regexp.MustCompile(`\[([^\]]*)\]\(([^)]+)\)`)
	// [text](#anchor) — anchor-only inline links.
	reAnchor = regexp.MustCompile(`\[([^\]]+)\]\((#[^)]*)\)`)
	// Reference definitions: [ref]: url
	reRefDef = regexp.MustCompile(`(?m)^\[([^\]]+)\]:\s+(\S+)`)
	// Reference usages: [text][ref]
	reRefUse = regexp.MustCompile(`\[([^\]]+)\]\[([^\]]*)\]`)
)

// ExtractMarkdown reads a Markdown file and returns all link occurrences.
// Start/End offsets are relative to the original file bytes and point to the
// URL value only (excluding surrounding delimiters).
func ExtractMarkdown(path string) ([]types.Link, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// ponytail: blank code regions so regexes don't match links inside them.
	masked := maskCodeRegions(data)

	var links []types.Link
	links = append(links, extractImages(masked, path, data)...)
	links = append(links, extractInline(masked, path, data)...)
	links = append(links, extractRefs(masked, path, data)...)
	return links, nil
}

// maskCodeRegions returns a copy of data where fenced blocks and inline code
// spans are replaced with spaces, preserving byte positions of everything else.
func maskCodeRegions(data []byte) []byte {
	out := make([]byte, len(data))
	copy(out, data)
	for _, loc := range reFenced.FindAllIndex(out, -1) {
		for i := loc[0]; i < loc[1]; i++ {
			out[i] = ' '
		}
	}
	for _, loc := range reInlineCode.FindAllIndex(out, -1) {
		for i := loc[0]; i < loc[1]; i++ {
			out[i] = ' '
		}
	}
	return out
}

func extractImages(masked []byte, path string, _ []byte) []types.Link {
	var links []types.Link
	for _, m := range reImage.FindAllSubmatchIndex(masked, -1) {
		// m[2]:m[3] = alt text, m[4]:m[5] = src url
		url := string(masked[m[4]:m[5]])
		links = append(links, types.Link{
			URL:        url,
			Text:       string(masked[m[2]:m[3]]),
			Type:       types.LinkTypeImage,
			SourceFile: path,
			Start:      m[4],
			End:        m[5],
		})
	}
	return links
}

func extractInline(masked []byte, path string, _ []byte) []types.Link {
	var links []types.Link
	for _, m := range reAnyInline.FindAllSubmatchIndex(masked, -1) {
		// Skip if preceded by '!' — that's an image, handled by extractImages.
		if m[0] > 0 && masked[m[0]-1] == '!' {
			continue
		}
		// m[2]:m[3] = link text, m[4]:m[5] = url
		url := string(masked[m[4]:m[5]])
		if strings.HasPrefix(url, "#") {
			continue // anchor-only, handled separately
		}
		links = append(links, types.Link{
			URL:        url,
			Text:       string(masked[m[2]:m[3]]),
			Type:       classifyURL(url),
			SourceFile: path,
			Start:      m[4],
			End:        m[5],
		})
	}
	// Anchor-only links.
	for _, m := range reAnchor.FindAllSubmatchIndex(masked, -1) {
		// m[2]:m[3] = link text, m[4]:m[5] = anchor
		url := string(masked[m[4]:m[5]])
		links = append(links, types.Link{
			URL:        url,
			Text:       string(masked[m[2]:m[3]]),
			Type:       types.LinkTypeAnchor,
			SourceFile: path,
			Start:      m[4],
			End:        m[5],
		})
	}
	return links
}

func extractRefs(masked []byte, path string, _ []byte) []types.Link {
	// Pass 1: collect definitions → url and its byte offsets.
	type defEntry struct {
		url   string
		start int
		end   int
	}
	defs := map[string]defEntry{}
	for _, m := range reRefDef.FindAllSubmatchIndex(masked, -1) {
		ref := strings.ToLower(string(masked[m[2]:m[3]]))
		defs[ref] = defEntry{
			url:   string(masked[m[4]:m[5]]),
			start: m[4],
			end:   m[5],
		}
	}
	if len(defs) == 0 {
		return nil
	}

	// Pass 2: resolve usages; offsets point to definition URL bytes.
	var links []types.Link
	for _, m := range reRefUse.FindAllSubmatchIndex(masked, -1) {
		refKey := strings.ToLower(string(masked[m[4]:m[5]]))
		if refKey == "" {
			// Collapsed reference [text][] — key is the text.
			refKey = strings.ToLower(string(masked[m[2]:m[3]]))
		}
		def, ok := defs[refKey]
		if !ok {
			continue
		}
		links = append(links, types.Link{
			URL:        def.url,
			Type:       classifyURL(def.url),
			SourceFile: path,
			Start:      def.start,
			End:        def.end,
		})
	}
	return links
}

func classifyURL(url string) types.LinkType {
	// Any URL with a scheme (http:, https:, mailto:, tel:, ftp:, etc.) is absolute.
	if i := strings.Index(url, "://"); i > 0 {
		return types.LinkTypeAbsolute
	}
	if strings.HasPrefix(url, "mailto:") || strings.HasPrefix(url, "tel:") {
		return types.LinkTypeAbsolute
	}
	return types.LinkTypeRelative
}
