package extractor

import (
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/validator"
)

var eligibleExts = map[string]bool{".md": true, ".html": true}

// skipDirs are directory names never scanned for documentation: dependency,
// build-output, cache, and VCS directories. Any dot-directory is also skipped
// (except the scan root itself).
var skipDirs = map[string]bool{
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"public":       true,
}

// Collect returns the paths of all .md and .html files under root.
// If dirs is non-empty, only files under those subdirectories (relative to root) are returned.
// sourceIgnore is a list of root-relative globs (e.g. "hugo/content/blog/*"); matching
// files are skipped and matching directories are pruned entirely, so no links are
// collected from them. Symlinks are never followed, and dependency/build/cache/VCS
// directories are skipped.
func Collect(root string, dirs, sourceIgnore []string) ([]string, error) {
	var paths []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip symlinks (both files and directories).
		if d.Type()&fs.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			// Never skip the scan root itself, even if named like a skip dir or hidden.
			if path == root {
				return nil
			}
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			// Prune whole subtrees the user has excluded from scanning.
			if matchesSourceIgnore(root, path, sourceIgnore) {
				return filepath.SkipDir
			}
			return nil
		}
		if !eligibleExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if len(dirs) > 0 && !underAnyDir(root, path, dirs) {
			return nil
		}
		if matchesSourceIgnore(root, path, sourceIgnore) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	return paths, err
}

// matchesSourceIgnore reports whether path (under root) matches any root-relative
// source-ignore glob. Matching is done on the slash-form path relative to root.
func matchesSourceIgnore(root, path string, sourceIgnore []string) bool {
	if len(sourceIgnore) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return validator.MatchesAnyPattern(filepath.ToSlash(rel), sourceIgnore)
}

func underAnyDir(root, path string, dirs []string) bool {
	for _, d := range dirs {
		prefix := filepath.Join(root, d) + string(filepath.Separator)
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
