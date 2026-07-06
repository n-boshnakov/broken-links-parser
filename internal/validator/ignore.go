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
// that apply only to links sourced from that repo.
type ScopedIgnoreFile struct {
	Global       []string
	RepoPatterns map[string][]string // key: absolute repo path
}

// LoadScopedIgnoreFile parses a sectioned .linkignore file of the form:
//
//	# Global patterns — applied to all links
//	mailto:*
//
//	[/absolute/path/to/repo]
//	/dev-setup/*
//	/example/*
//
// Lines starting with '#' and blank lines are ignored.
// Patterns before the first [...] section are global.
// Returns an empty struct (not an error) if the file does not exist.
func LoadScopedIgnoreFile(path string) (ScopedIgnoreFile, error) {
	result := ScopedIgnoreFile{
		RepoPatterns: make(map[string][]string),
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	defer f.Close()

	currentSection := "" // empty = global
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			// New repo section — value is an absolute path.
			currentSection = filepath.Clean(line[1 : len(line)-1])
			continue
		}
		if currentSection == "" {
			result.Global = append(result.Global, line)
		} else {
			result.RepoPatterns[currentSection] = append(result.RepoPatterns[currentSection], line)
		}
	}
	return result, scanner.Err()
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
