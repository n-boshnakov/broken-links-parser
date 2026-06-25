package types

// LinkType classifies the kind of link found in a source file.
type LinkType string

const (
	LinkTypeRelative LinkType = "relative"
	LinkTypeAbsolute LinkType = "absolute"
	LinkTypeAnchor   LinkType = "anchor"
	LinkTypeImage    LinkType = "image"
)

// Link is a single link occurrence extracted from a source file.
// Start and End are byte offsets of the URL value within the file
// (excluding surrounding delimiters such as quotes or parentheses).
// Repairs must be applied in reverse offset order to keep earlier offsets valid.
type Link struct {
	URL        string
	Type       LinkType
	SourceFile string
	Start      int
	End        int
}
