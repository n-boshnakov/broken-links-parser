package validator

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// LoadIgnoreFile reads a .linkignore-style file and returns the patterns in it.
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
	// Build regexp: escape everything, then replace \* → .* and \? → [^/]
	escaped := regexp.QuoteMeta(pattern)
	escaped = strings.ReplaceAll(escaped, `\*`, `.*`)
	escaped = strings.ReplaceAll(escaped, `\?`, `[^/]`)
	re, err := regexp.Compile(`^` + escaped + `$`)
	if err != nil {
		return false
	}
	return re.MatchString(s)
}
