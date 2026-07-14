package validator

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

var (
	reMDHeading     = regexp.MustCompile(`^#{1,6}\s+(.+)`)
	reHTMLHeading   = regexp.MustCompile(`(?i)<h[1-6][^>]*>([^<]+)</h[1-6]>`)
	reHTMLHeadingID = regexp.MustCompile(`(?i)<h[1-6][^>]*\sid="([^"]+)"`)
	reNonAlnum      = regexp.MustCompile(`[^\p{L}\p{N}\- ]`)
	reLineRange     = regexp.MustCompile(`^L\d+(-L\d+)?$`) // GitHub line-range anchors: L48 or L48-L55
	reMDLink        = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`) // [text](url) → text
	reBacktick      = regexp.MustCompile("`([^`]*)`")              // `code` → code (keep inner text)
)

// NormaliseAnchor converts a heading string to its GitHub-flavored anchor form.
// Strips Markdown link syntax and backtick code spans before normalising.
func NormaliseAnchor(heading string) string {
	// Strip Markdown links [text](url) → text
	s := reMDLink.ReplaceAllString(heading, "$1")
	// Strip any orphaned ](url) suffix left after partial link extraction.
	if i := strings.Index(s, "]("); i >= 0 {
		s = s[:i]
	}
	// Unwrap backtick code spans: `code` → code (GitHub keeps inner text)
	s = reBacktick.ReplaceAllString(s, "$1")
	s = strings.ToLower(s)
	s = strings.TrimSpace(s) // trim whitespace before converting spaces to hyphens
	s = reNonAlnum.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, " ", "-")
	// Do NOT trim hyphens or collapse "--": GitHub preserves hyphens from stripped
	// special chars (e.g. `["Care" text]` → "-care--text", "A & B" → "a--b").
	return s
}

// ExtractAnchors returns the normalised anchor IDs of all headings in path,
// including numbered variants for duplicate headings (e.g. "foo", "foo-1", "foo-2").
// Supports Markdown (# headings) and HTML (<h1>–<h6> tags).
func ExtractAnchors(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	content := string(data)

	var base []string
	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".md" {
		scanner := bufio.NewScanner(strings.NewReader(content))
		for scanner.Scan() {
			if m := reMDHeading.FindStringSubmatch(scanner.Text()); m != nil {
				base = append(base, NormaliseAnchor(m[1]))
			}
		}
	}

	// id attributes are used verbatim by GitHub as URL anchors — do not normalise.
	for _, m := range reHTMLHeadingID.FindAllStringSubmatch(content, -1) {
		base = append(base, m[1])
	}
	for _, m := range reHTMLHeading.FindAllStringSubmatch(content, -1) {
		base = append(base, NormaliseAnchor(m[1]))
	}

	// GitHub disambiguates duplicate anchors by appending -1, -2, ... starting
	// from the second occurrence. Build the full set including numbered variants.
	seen := make(map[string]int)
	anchors := make([]string, 0, len(base))
	for _, a := range base {
		anchors = append(anchors, a)
		n := seen[a]
		seen[a]++
		if n > 0 {
			// This is the (n+1)th occurrence; the previous one was numbered n-1 (or bare).
			// Emit the numbered form for this occurrence.
			anchors = append(anchors, a+"-"+strconv.Itoa(n))
		}
	}

	return anchors, nil
}

// ValidateRelative checks a relative or anchor-only link against the local filesystem.
// repoRoot is the root of the scanned repository; used to resolve links starting with /.
func ValidateRelative(link types.Link, patterns []string, repoRoot string) types.ValidationResult {
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
		if strings.HasPrefix(url, "/") {
			// Absolute-path link (repo-root-relative).
			// Prefer SourceRepo (for docforge-sourced files), then repoRoot.
			base := link.SourceRepo
			if base == "" {
				base = repoRoot
			}
			if base != "" {
				targetPath = filepath.Join(base, filepath.FromSlash(url))
			} else {
				// No root known — treat as relative to source file directory.
				targetPath = filepath.Join(filepath.Dir(link.SourceFile), filepath.FromSlash(url))
			}
		} else {
			targetPath = filepath.Join(filepath.Dir(link.SourceFile), filepath.FromSlash(url))
		}
	}

	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonFileNotFound}
	}

	if fragment != "" {
		// GitHub line-range anchors (L48, L48-L55) are always valid — skip heading check.
		if reLineRange.MatchString(fragment) {
			return types.ValidationResult{Link: link, Valid: true}
		}
		// Normalise the fragment the same way headings are normalised,
		// so special chars like & and . in the fragment are handled correctly.
		normFragment := NormaliseAnchor(fragment)
		anchors, err := ExtractAnchors(targetPath)
		if err != nil {
			return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonFileNotFound}
		}
		for _, a := range anchors {
			if a == normFragment {
				return types.ValidationResult{Link: link, Valid: true}
			}
		}
		// No exact match — find the closest anchor as a suggestion.
		suggested := closestAnchor(normFragment, anchors)
		return types.ValidationResult{
			Link:            link,
			Valid:           false,
			Reason:          types.ReasonAnchorNotFound,
			SuggestedAnchor: suggested,
		}
	}

	return types.ValidationResult{Link: link, Valid: true}
}

// closestAnchor returns the best matching anchor from candidates for the given fragment.
// Tries passes in order: prefix, substring, reverse-substring, reverse-prefix, Levenshtein.
func closestAnchor(fragment string, candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}

	// Pass 1: fragment is a prefix of a candidate (e.g. #foo → #foo-deprecated).
	// Pick the shortest such candidate.
	prefixBest := ""
	for _, c := range candidates {
		if strings.HasPrefix(c, fragment+"-") || strings.HasPrefix(c, fragment+"_") {
			if prefixBest == "" || len(c) < len(prefixBest) {
				prefixBest = c
			}
		}
	}
	if prefixBest != "" {
		return prefixBest
	}

	// Pass 2: fragment is a substring of a candidate
	// (e.g. #custom-domains → #using-a-custom-domains-issuer).
	// Pick the shortest candidate that contains the fragment.
	subBest := ""
	for _, c := range candidates {
		if strings.Contains(c, fragment) {
			if subBest == "" || len(c) < len(subBest) {
				subBest = c
			}
		}
	}
	if subBest != "" {
		return subBest
	}

	// Pass 3: candidate is a substring of fragment
	// (e.g. #use-case-3-monitoring-backup-health → #monitoring-backup-health).
	// Pick the longest candidate that is contained in the fragment.
	revSubBest := ""
	for _, c := range candidates {
		if strings.Contains(fragment, c) {
			if revSubBest == "" || len(c) > len(revSubBest) {
				revSubBest = c
			}
		}
	}
	if revSubBest != "" {
		return revSubBest
	}

	// Pass 4: candidate is a prefix of fragment
	// (e.g. #networkpolicy-controller-registrar → #networkpolicy-controller).
	revPrefixBest := ""
	for _, c := range candidates {
		if strings.HasPrefix(fragment, c+"-") || strings.HasPrefix(fragment, c+"_") {
			if revPrefixBest == "" || len(c) > len(revPrefixBest) {
				revPrefixBest = c
			}
		}
	}
	if revPrefixBest != "" {
		return revPrefixBest
	}

	// Pass 5: Levenshtein distance.
	best := ""
	bestDist := len(fragment) + 1
	for _, c := range candidates {
		d := levenshtein(fragment, c)
		if d < bestDist {
			bestDist = d
			best = c
		}
	}
	threshold := len(fragment)/2 + 1
	// Raise cap for longer fragments to allow single-word insertions.
	if threshold > 12 {
		threshold = 12
	}
	if bestDist <= threshold && best != "" {
		return best
	}

	// Pass 6: word-token overlap — handles word-reorder and leading-dash fragments.
	// Requires ≥2 shared tokens and overlap/len(fragment tokens) ≥ 0.5.
	fragTokens := filterEmpty(strings.Split(fragment, "-"))
	if len(fragTokens) >= 2 {
		bestOverlap, bestRatio := "", 0.0
		for _, c := range candidates {
			ct := filterEmpty(strings.Split(c, "-"))
			shared := tokenOverlap(fragTokens, ct)
			ratio := float64(shared) / float64(len(fragTokens))
			if shared >= 2 && ratio >= 0.5 {
				if ratio > bestRatio || (ratio == bestRatio && len(c) < len(bestOverlap)) {
					bestRatio = ratio
					bestOverlap = c
				}
			}
		}
		if bestOverlap != "" {
			return bestOverlap
		}
	}
	return ""
}

// levenshtein computes the edit distance between two strings.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	row := make([]int, lb+1)
	for j := range row {
		row[j] = j
	}
	for i := 1; i <= la; i++ {
		prev := row[0]
		row[0] = i
		for j := 1; j <= lb; j++ {
			tmp := row[j]
			if ra[i-1] == rb[j-1] {
				row[j] = prev
			} else {
				row[j] = 1 + min3(prev, row[j], row[j-1])
			}
			prev = tmp
		}
	}
	return row[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

func filterEmpty(ss []string) []string {
	out := ss[:0]
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func tokenOverlap(a, b []string) int {
	m := make(map[string]bool, len(b))
	for _, s := range b {
		m[s] = true
	}
	n := 0
	for _, s := range a {
		if m[s] {
			n++
		}
	}
	return n
}
