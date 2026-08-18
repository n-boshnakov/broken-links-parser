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
	StrategyWaybackAI  = "wayback+ai"
)

// Confidence constants for ResolutionResult.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// ConfidenceLabel maps a numeric confidence score (0.0–1.0) to a label.
// It is the single source of truth for the score → label mapping.
func ConfidenceLabel(score float64) string {
	switch {
	case score >= 0.8:
		return ConfidenceHigh
	case score >= 0.5:
		return ConfidenceMedium
	default:
		return ConfidenceLow
	}
}

// UnresolvedReason constants explain why no fix was found for a broken link.
const (
	UnresolvedNoHistory      = "NO_HISTORY"         // file not found in git history (may never have existed)
	UnresolvedAPIBlocked     = "API_BLOCKED"         // GitHub API returned 403 (token policy, permissions)
	UnresolvedAPIRateLimit   = "API_RATE_LIMITED"    // GitHub API rate limit exhausted
	UnresolvedRepoNotFound   = "REPO_NOT_FOUND"      // org/repo does not exist or is private
	UnresolvedAmbiguous      = "AMBIGUOUS"           // multiple files with same name found, cannot determine correct one
	UnresolvedNoCloneNoAPI   = "NO_CLONE_NO_API"     // no local clone and API unavailable
	UnresolvedExternalNoAI      = "EXTERNAL_NO_AI"      // non-GitHub link and --ai not enabled
	UnresolvedBotBlocked        = "BOT_BLOCKED"          // server returned 403/429 — likely works in browser, AI skipped
	UnresolvedAIFailed          = "AI_FAILED"            // AI was enabled but returned no useful result
	UnresolvedAIInvalidURL      = "AI_INVALID_URL"          // AI returned a syntactically invalid URL
	UnresolvedAINoValidCandidate = "AI_NO_VALID_CANDIDATE"  // AI returned candidates but all failed HTTP validation
	UnresolvedAIAuthError       = "AI_AUTH_ERROR"           // API key is missing, invalid, or rejected
	UnresolvedSourceMalformed   = "SOURCE_MALFORMED"        // the original broken URL is itself malformed
	UnresolvedUnsupportedGitHubURL = "UNSUPPORTED_GITHUB_URL" // GitHub URL is not a resolvable file link (e.g. /tree/, /issues/, repo root, profile)
)

// Candidate is one considered replacement URL for a broken link, retained so the
// report can show alternatives and the reasoning behind each, not just the winner.
type Candidate struct {
	URL             string
	Strategy        string  // one of the Strategy* constants
	Reasoning       string  // why this candidate was suggested (from the AI)
	ConfidenceScore float64 // 0.0–1.0
	Reachable       bool    // true when the URL passed an HTTP reachability check
}

// ResolutionResult is the outcome of attempting to find a replacement for a broken link.
type ResolutionResult struct {
	ValidationResult
	FixedURL          string      // empty if unresolved
	Strategy          string      // one of the Strategy* constants
	Confidence        string      // derived label; use ConfidenceLabel(ConfidenceScore)
	ConfidenceScore   float64     // 0.0–1.0 numeric confidence; source of truth for Confidence
	Reasoning         string      // human-readable explanation for the chosen fix (from the AI)
	Candidates        []Candidate // all candidates considered, best first; may be empty
	Deleted           bool        // true when FixedURL points to a deletion commit rather than a replacement
	UnresolvedReason  string      // one of the Unresolved* constants, set when FixedURL is empty
	IsWaybackFallback bool        // true when FixedURL is a Wayback archive URL used as last-resort
}

// Reason codes for ValidationResult.
const (
	ReasonFileNotFound  = "FILE_NOT_FOUND"
	ReasonAnchorNotFound = "ANCHOR_NOT_FOUND"
	ReasonHTTPError     = "HTTP_ERROR"
	ReasonTimeout       = "TIMEOUT"
	ReasonAuthBlocked   = "AUTH_BLOCKED"   // 401/403 — page likely exists but rejects automated access
	ReasonNetworkError  = "NETWORK_ERROR"  // DNS failure, connection refused, TLS/certificate error
	ReasonIgnored       = "IGNORED"
)

// ValidationResult is the outcome of validating a single Link.
type ValidationResult struct {
	Link                 Link
	Valid                bool
	Reason               string  // one of the Reason* constants, empty when Valid
	StatusCode           int     // HTTP status code, 0 for non-HTTP checks
	NotAssembled         bool    // true when link is valid on disk but target not in docforge manifest (--docforge-strict only)
	SuggestedAnchor      string  // closest matching anchor when Reason is ANCHOR_NOT_FOUND; empty otherwise
	SuggestedAnchorScore float64 // 0.0–1.0 confidence of the suggested anchor; 0 when none
	SuggestedTargetPath  string  // resolved on-disk file the anchor lives in, relative to the scan root (slash form); empty when the target is the source file itself
}

// IsBroken reports whether a result should count as a broken link. Valid links,
// ignored links, and non-broken warning states (auth-restricted, network error)
// are excluded — those are surfaced separately rather than counted as breakage.
func (r ValidationResult) IsBroken() bool {
	if r.Valid {
		return false
	}
	switch r.Reason {
	case ReasonIgnored, ReasonAuthBlocked, ReasonNetworkError:
		return false
	}
	return true
}

// Link is a single link occurrence extracted from a source file.
// Start and End are byte offsets of the URL value within the file
// (excluding surrounding delimiters such as quotes or parentheses).
// Repairs must be applied in reverse offset order to keep earlier offsets valid.
type Link struct {
	URL        string
	Text       string   // anchor text or alt text — empty for reference-style links
	Type       LinkType
	SourceFile string
	SourceRepo string   // local clone path of the origin repo; empty for files in the primary scanned tree
	Start      int
	End        int
}
