package validator

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ScopedIgnoreFile holds the parsed result of a sectioned .linkignore file.
// Global patterns apply to all links. RepoPatterns maps absolute repo path → patterns
// that apply only to links sourced from that repo. SourcePatterns are root-relative
// source-path globs whose matching files/folders are skipped entirely at scan time
// (no links collected from them).
type ScopedIgnoreFile struct {
	Global             []string
	RepoPatterns       map[string][]string // URL-ignore patterns, key: absolute repo path
	RepoSourcePatterns map[string][]string // source-skip globs (repo-relative), key: absolute repo path
	SourcePatterns     []string            // root-relative source-path globs (from the [sources] section)
}

// LoadScopedIgnoreFile parses a sectioned .linkignore file of the form:
//
//	# Global patterns — applied to all link URLs
//	mailto:*
//
//	[/absolute/path/to/repo]
//	/dev-setup/*
//	/example/*
//
//	[sources]
//	# root-relative source files/folders to skip entirely (no links collected)
//	hugo/content/blog/*
//	hugo/content/community/*
//
// Lines starting with '#' and blank lines are ignored.
// Patterns before the first [...] section are global (matched against link URLs).
// The special [sources] section holds root-relative source-path globs; every other
// [...] header is an absolute repo path whose patterns are matched against link URLs.
// Returns an empty struct (not an error) if the file does not exist.
func LoadScopedIgnoreFile(path string) (ScopedIgnoreFile, error) {
	result := ScopedIgnoreFile{
		RepoPatterns:       make(map[string][]string),
		RepoSourcePatterns: make(map[string][]string),
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer f.Close()

	const sourcesSection = "\x00sources" // sentinel distinct from any repo path
	currentSection := ""                 // empty = global
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			header := line[1 : len(line)-1]
			if header == "sources" {
				currentSection = sourcesSection
			} else {
				// Repo section — value is an absolute path.
				currentSection = filepath.Clean(header)
			}
			continue
		}
		switch currentSection {
		case "":
			result.Global = append(result.Global, line)
		case sourcesSection:
			result.SourcePatterns = append(result.SourcePatterns, line)
		default:
			// Within a repo section, a "source: <glob>" line is a per-repo source-skip
			// glob (matched against the source path at scan time); every other line is a
			// URL-ignore pattern (matched against link URLs during validation).
			if glob, ok := cutSourcePrefix(line); ok {
				result.RepoSourcePatterns[currentSection] = append(result.RepoSourcePatterns[currentSection], glob)
			} else {
				result.RepoPatterns[currentSection] = append(result.RepoPatterns[currentSection], line)
			}
		}
	}
	return result, scanner.Err()
}

// cutSourcePrefix reports whether line is a per-repo source-skip directive
// ("source: <glob>") and returns the trimmed glob. The keyword is case-insensitive
// and tolerant of a missing space after the colon.
func cutSourcePrefix(line string) (string, bool) {
	const kw = "source:"
	if len(line) >= len(kw) && strings.EqualFold(line[:len(kw)], kw) {
		return strings.TrimSpace(line[len(kw):]), true
	}
	return "", false
}

// PatternsFor returns the combined set of patterns that apply to a link
// given its source file path. Global patterns always apply; repo-specific
// patterns apply when sourceFile is under the repo path.
func (s ScopedIgnoreFile) PatternsFor(sourceFile string) []string {
	patterns := append([]string(nil), s.Global...)
	for repoPath, repoPatterns := range s.RepoPatterns {
		// Match if sourceFile is under repoPath.
		clean := filepath.Clean(sourceFile)
		prefix := repoPath + string(filepath.Separator)
		if clean == repoPath || strings.HasPrefix(clean, prefix) {
			patterns = append(patterns, repoPatterns...)
		}
	}
	return patterns
}

// SourceSkipGlobsFor returns the effective root-relative source-skip globs for a scan
// rooted at root: the global [sources] patterns plus every applicable per-repo
// "source:" glob, rebased so it can be matched against paths relative to root (the form
// the collector uses). A repo's globs are included only when root and the repo path lie
// on the same directory chain; when root is deeper than the repo, globs outside the
// scanned subtree are dropped.
func (s ScopedIgnoreFile) SourceSkipGlobsFor(root string) []string {
	globs := append([]string(nil), s.SourcePatterns...)
	R := filepath.Clean(root)
	for repoPath, repoGlobs := range s.RepoSourcePatterns {
		P := filepath.Clean(repoPath)
		switch {
		case R == P:
			globs = append(globs, repoGlobs...)
		case isUnder(R, P): // root is inside the repo
			sub := filepath.ToSlash(mustRel(P, R))
			for _, g := range repoGlobs {
				if rebased, ok := stripDirPrefix(g, sub); ok {
					globs = append(globs, rebased)
				}
			}
		case isUnder(P, R): // repo is inside the root
			sub := filepath.ToSlash(mustRel(R, P))
			if sub != "" {
				for _, g := range repoGlobs {
					globs = append(globs, sub+"/"+g)
				}
			}
		// else: unrelated trees → drop this repo's globs.
		}
	}
	return globs
}

// isUnder reports whether child is strictly within parent (segment-aware).
func isUnder(child, parent string) bool {
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// mustRel returns filepath.Rel(base, target) or "" on error.
func mustRel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return ""
	}
	return rel
}

// stripDirPrefix removes a leading "sub/" directory prefix from a slash-form glob.
// Returns (glob, true) when sub is empty/".", (trimmed, true) when the glob is segment-
// prefixed by sub, and ("", false) when the glob falls outside sub.
func stripDirPrefix(glob, sub string) (string, bool) {
	if sub == "" || sub == "." {
		return glob, true
	}
	if glob == sub {
		return "", false // the dir itself, no file under it
	}
	if strings.HasPrefix(glob, sub+"/") {
		return strings.TrimPrefix(glob, sub+"/"), true
	}
	return "", false
}

// LoadIgnoreFile reads a simple (non-sectioned) .linkignore file and returns patterns.
// Lines starting with '#' and blank lines are ignored.
// Returns nil (not an error) if the file does not exist.
func LoadIgnoreFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var patterns []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns, scanner.Err()
}

// MatchesAnyPattern reports whether url matches at least one pattern.
// Patterns support '*' as a wildcard that matches any sequence of characters
// (including '/'). '?' matches any single character except '/'.
func MatchesAnyPattern(url string, patterns []string) bool {
	for _, p := range patterns {
		if matchPattern(p, url) {
			return true
		}
	}
	return false
}

// matchPattern converts a simple glob (*, ?) to a regexp and tests it against s.
func matchPattern(pattern, s string) bool {
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, `\*`, `.*`)
	escaped = strings.ReplaceAll(escaped, `\?`, `[^/]`)
	re, err := regexp.Compile(`^` + escaped + `$`)
	if err != nil {
		return false
	}
	return re.MatchString(s)
}
