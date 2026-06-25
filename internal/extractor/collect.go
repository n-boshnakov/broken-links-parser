package extractor

import (
	"io/fs"
	"path/filepath"
	"strings"
)

var eligibleExts = map[string]bool{".md": true, ".html": true}

// Collect returns the paths of all .md and .html files under root.
// If dirs is non-empty, only files under those subdirectories (relative to root) are returned.
// Symlinks are never followed.
func Collect(root string, dirs []string) ([]string, error) {
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
			return nil
		}
		if !eligibleExts[strings.ToLower(filepath.Ext(path))] {
			return nil
		}
		if len(dirs) > 0 && !underAnyDir(root, path, dirs) {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	return paths, err
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
