package extractor

import (
	"os"
	"strings"

	"golang.org/x/net/html"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// ExtractHTML reads an HTML file and returns all <a href> and <img src> links.
// Malformed HTML is tolerated — tokenization continues past errors.
// Start/End offsets point to the attribute value bytes (excluding quotes).
func ExtractHTML(path string) ([]types.Link, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var links []types.Link
	z := html.NewTokenizer(strings.NewReader(string(data)))

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break // EOF or unrecoverable error — tolerated per spec.
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}

		tok := z.Token()
		switch tok.Data {
		case "a":
			if lnk, ok := attrLink(data, tok, "href", path, false); ok {
				links = append(links, lnk)
			}
		case "img":
			if lnk, ok := attrLink(data, tok, "src", path, true); ok {
				links = append(links, lnk)
			}
		}
	}
	return links, nil
}

// attrLink finds the named attribute in tok, validates it is non-empty,
// locates its byte offset in the original file data, and returns a Link.
// isImage forces LinkTypeImage regardless of the URL value.
func attrLink(data []byte, tok html.Token, attrName, path string, isImage bool) (types.Link, bool) {
	var val string
	for _, a := range tok.Attr {
		if a.Key == attrName {
			val = a.Val
			break
		}
	}
	if val == "" {
		return types.Link{}, false
	}

	linkType := classifyHTMLURL(val)
	if isImage {
		linkType = types.LinkTypeImage
	}

	// Locate the attribute value bytes in the original file.
	// Search for the pattern: attrName="val" or attrName='val'
	start, end := findAttrValueOffset(data, attrName, val)

	return types.Link{
		URL:        val,
		Type:       linkType,
		SourceFile: path,
		Start:      start,
		End:        end,
	}, true
}

// findAttrValueOffset finds the byte range of the attribute value in data.
// It searches for attrName="val" or attrName='val' and returns the offsets of
// val only (excluding quotes). Returns (-1, -1) if not found.
func findAttrValueOffset(data []byte, attrName, val string) (int, int) {
	needle := attrName + `="` + val + `"`
	if idx := strings.Index(string(data), needle); idx >= 0 {
		start := idx + len(attrName) + 2 // skip attrName + ="
		return start, start + len(val)
	}
	needle = attrName + `='` + val + `'`
	if idx := strings.Index(string(data), needle); idx >= 0 {
		start := idx + len(attrName) + 2
		return start, start + len(val)
	}
	return -1, -1
}

func classifyHTMLURL(url string) types.LinkType {
	if strings.HasPrefix(url, "#") {
		return types.LinkTypeAnchor
	}
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return types.LinkTypeAbsolute
	}
	return types.LinkTypeRelative
}
