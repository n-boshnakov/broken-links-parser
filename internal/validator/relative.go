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
// rootRelativeBase, when non-empty, overrides the base for /-prefixed links so they
// resolve against a content root (e.g. a docs content directory) rather than the repo
// root — links in many docs sites are written root-relative to the content tree.
func ValidateRelative(link types.Link, patterns []string, repoRoot, rootRelativeBase string) types.ValidationResult {
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
			// Root-relative link. Prefer an explicit content root, then SourceRepo
			// (for docforge-sourced files), then the repo root.
			base := rootRelativeBase
			if base == "" {
				base = link.SourceRepo
			}
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

	// Resolve the target on disk. For anchor-only links (url == "") the target is
	// the source file itself and must exist as-is. Otherwise, if the exact path is
	// missing, fall back to static-site route conventions (extensionless routes map
	// to a .md file or an index.md/_index.md inside a directory of that name).
	if url == "" {
		if _, err := os.Stat(targetPath); os.IsNotExist(err) {
			return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonFileNotFound}
		}
	} else if resolved, kind := resolveTarget(targetPath); kind == targetFile {
		targetPath = resolved
	} else if kind == targetDir {
		// Link points to a real directory with no index page (e.g. a source-code
		// folder). On GitHub/GitLab such a link renders the directory listing, so
		// it is valid. A fragment can't resolve against a bare directory — treat the
		// directory itself as the target and skip the heading check.
		return types.ValidationResult{Link: link, Valid: true}
	} else {
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
		// No exact match — find the closest anchor as a suggestion. Carry the resolved
		// target file (relative to repoRoot, slash form) so the report can build a
		// clickable URL to the real file rather than the extensionless route. Empty when
		// the anchor is in the source file itself, or when the target isn't under repoRoot.
		suggested, score := closestAnchorScored(normFragment, anchors)
		var targetRel string
		if repoRoot != "" && targetPath != link.SourceFile {
			if rel, err := filepath.Rel(repoRoot, targetPath); err == nil && !strings.HasPrefix(rel, "..") {
				targetRel = filepath.ToSlash(rel)
			}
		}
		return types.ValidationResult{
			Link:                 link,
			Valid:                false,
			Reason:               types.ReasonAnchorNotFound,
			SuggestedAnchor:      suggested,
			SuggestedAnchorScore: score,
			SuggestedTargetPath:  targetRel,
		}
	}

	return types.ValidationResult{Link: link, Valid: true}
}

// targetKind classifies the outcome of resolving a local link target.
type targetKind int

const (
	targetNone targetKind = iota // nothing exists at the target
	targetFile                   // resolved to a content file (path returned)
	targetDir                    // an existing directory with no index page
)

// resolveTarget locates the on-disk target of a local link, applying the route
// conventions shared by static-site generators (Hugo, Jekyll, VitePress, Docusaurus,
// MkDocs, …): a rendered route such as "/docs/foo/" maps to "foo.md", or to
// "index.md"/"_index.md" inside a "foo" directory.
//
// Returns (resolvedFilePath, targetFile) when a content file is found,
// ("", targetDir) when the target is a real directory with no index page (still a
// valid link on GitHub/GitLab, which renders the directory listing), and
// ("", targetNone) when nothing exists.
//
// Order tried:
//  1. the path exactly as written (a real file)
//  2. if it's a directory: <dir>/index.md, <dir>/_index.md, else targetDir
//  3. otherwise (unless it already names a content file): <path>.md, then
//     <path>/index.md, <path>/_index.md
func resolveTarget(target string) (string, targetKind) {
	if fi, err := os.Stat(target); err == nil {
		if !fi.IsDir() {
			return target, targetFile
		}
		// Directory: prefer an index file inside it, else accept the directory.
		for _, idx := range []string{"index.md", "_index.md"} {
			p := filepath.Join(target, idx)
			if _, err := os.Stat(p); err == nil {
				return p, targetFile
			}
		}
		return "", targetDir
	}

	// Not found as-is. Treat it as a rendered route and try the content-file
	// conventions — but skip this if it already names a content file (which would
	// have matched above), since then it's a genuine miss. Note: a dot in the final
	// segment (e.g. "12.25-gardener-cookies") is NOT a file extension, so we can't
	// gate on filepath.Ext being empty.
	if ext := strings.ToLower(filepath.Ext(target)); ext != ".md" && ext != ".html" {
		for _, cand := range []string{
			target + ".md",
			filepath.Join(target, "index.md"),
			filepath.Join(target, "_index.md"),
		} {
			if _, err := os.Stat(cand); err == nil {
				return cand, targetFile
			}
		}
	}
	return "", targetNone
}

// closestAnchor returns the best matching anchor from candidates for the given fragment.
// Tries passes in order: prefix, substring, reverse-substring, reverse-prefix, Levenshtein.
func closestAnchor(fragment string, candidates []string) string {
	s, _ := closestAnchorScored(fragment, candidates)
	return s
}

// closestAnchorScored is closestAnchor with a 0.0–1.0 confidence for the match.
// The passes and their order are identical to closestAnchor (so the pinned per-pass
// behaviour is preserved); each pass carries a score reflecting how strong its match is.
// Returns ("", 0) when no pass matches.
func closestAnchorScored(fragment string, candidates []string) (string, float64) {
	if len(candidates) == 0 {
		return "", 0
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
		return prefixBest, 0.9
	}

	// Pass 2: fragment is a substring of a candidate
	// (e.g. #custom-domains → #using-a-custom-domains-issuer).
	// Pick the shortest candidate that contains the fragment. The score reflects how
	// much of the candidate the fragment actually covers: a fragment that is nearly
	// the whole candidate is a strong match, while a short fragment buried in a long
	// candidate (e.g. #garden inside #gardener-discovery-server) is weak.
	subBest := ""
	for _, c := range candidates {
		if strings.Contains(c, fragment) {
			if subBest == "" || len(c) < len(subBest) {
				subBest = c
			}
		}
	}
	if subBest != "" {
		return subBest, substringScore(fragment, subBest)
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
		score := substringScore(fragment, revSubBest)
		// Penalise dropping the fragment's distinctive trailing token (e.g. losing
		// ".Cluster" from an API-type anchor) — the candidate names a broader section.
		if droppedTrailingToken(fragment, revSubBest) {
			score *= 0.85
		}
		return revSubBest, score
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
		score := 0.9
		// A prefix match that drops the fragment's distinctive trailing token points
		// at a broader parent section — down-weight it below a full-strength match.
		if droppedTrailingToken(fragment, revPrefixBest) {
			score *= 0.85
		}
		return revPrefixBest, score
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
		// A near-identical anchor (typo, singular/plural) is a strong match; a larger
		// edit distance is a weak guess and must stay below the report's fix gate so it
		// renders as a hint rather than a confident fix.
		var score float64
		if bestDist <= 2 {
			score = 0.85
		} else {
			score = 0.4 + 0.4*(1-float64(bestDist)/float64(threshold))
			if score < 0.3 {
				score = 0.3
			}
			if score > 0.6 {
				score = 0.6
			}
		}
		return best, score
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
			return bestOverlap, 0.5 + 0.4*bestRatio
		}
	}
	return "", 0
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

// substringScore scores a containment match (one of fragment/candidate contains the
// other) by how much of the longer string the shorter one covers. A near-equal length
// ratio is a strong match; a short string buried in a long one is weak. The band
// 0.45–0.9 keeps high-coverage matches above the report fix gate (0.7) and pushes
// low-coverage ones (e.g. "garden" in "gardener-discovery-server") below it.
func substringScore(fragment, candidate string) float64 {
	shorter, longer := len(fragment), len(candidate)
	if shorter > longer {
		shorter, longer = longer, shorter
	}
	if longer == 0 {
		return 0.45
	}
	ratio := float64(shorter) / float64(longer)
	return 0.45 + 0.45*ratio
}

// droppedTrailingToken reports whether candidate is fragment with its last
// hyphen-delimited token removed — i.e. the suggestion loses the fragment's most
// distinctive trailing qualifier and names a broader section instead.
func droppedTrailingToken(fragment, candidate string) bool {
	ft := filterEmpty(strings.Split(fragment, "-"))
	ct := filterEmpty(strings.Split(candidate, "-"))
	if len(ft) < 2 || len(ct) != len(ft)-1 {
		return false
	}
	for i := range ct {
		if ct[i] != ft[i] {
			return false
		}
	}
	return true
}
