package types

// LinkType classifies the kind of link found in a source file.
type LinkType string

const (
	LinkTypeRelative LinkType = "relative"
	LinkTypeAbsolute LinkType = "absolute"
	LinkTypeAnchor   LinkType = "anchor"
	LinkTypeImage    LinkType = "image"
)

// Strategy constants for ResolutionResult.
const (
	StrategyGitHistory = "git-history"
	StrategyLocalClone = "local-clone"
	StrategyGitHubAPI  = "github-api"
	StrategyAI         = "ai"
)

// Confidence constants for ResolutionResult.
const (
	ConfidenceHigh = "high"
	ConfidenceLow  = "low"
)

// UnresolvedReason constants explain why no fix was found for a broken link.
const (
	UnresolvedNoHistory      = "NO_HISTORY"         // file not found in git history (may never have existed)
	UnresolvedAPIBlocked     = "API_BLOCKED"         // GitHub API returned 403 (token policy, permissions)
	UnresolvedAPIRateLimit   = "API_RATE_LIMITED"    // GitHub API rate limit exhausted
	UnresolvedRepoNotFound   = "REPO_NOT_FOUND"      // org/repo does not exist or is private
	UnresolvedAmbiguous      = "AMBIGUOUS"           // multiple files with same name found, cannot determine correct one
	UnresolvedNoCloneNoAPI   = "NO_CLONE_NO_API"     // no local clone and API unavailable
	UnresolvedExternalNoAI   = "EXTERNAL_NO_AI"      // non-GitHub link and --ai not enabled
	UnresolvedAIFailed       = "AI_FAILED"           // AI was enabled but returned no useful result
)

// ResolutionResult is the outcome of attempting to find a replacement for a broken link.
type ResolutionResult struct {
	ValidationResult
	FixedURL         string // empty if unresolved
	Strategy         string // one of the Strategy* constants
	Confidence       string // ConfidenceHigh or ConfidenceLow
	Deleted          bool   // true when FixedURL points to a deletion commit rather than a replacement
	UnresolvedReason string // one of the Unresolved* constants, set when FixedURL is empty
}

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
