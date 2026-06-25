package types

// LinkType classifies the kind of link found in a source file.
type LinkType string

const (
	LinkTypeRelative LinkType = "relative"
	LinkTypeAbsolute LinkType = "absolute"
	LinkTypeAnchor   LinkType = "anchor"
	LinkTypeImage    LinkType = "image"
)

// Reason codes for ValidationResult.
const (
	ReasonFileNotFound  = "FILE_NOT_FOUND"
	ReasonAnchorNotFound = "ANCHOR_NOT_FOUND"
	ReasonHTTPError     = "HTTP_ERROR"
	ReasonTimeout       = "TIMEOUT"
	ReasonIgnored       = "IGNORED"
)

// ValidationResult is the outcome of validating a single Link.
type ValidationResult struct {
	Link       Link
	Valid      bool
	Reason     string // one of the Reason* constants, empty when Valid
	StatusCode int    // HTTP status code, 0 for non-HTTP checks
}

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
