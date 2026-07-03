package docforge

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SourceEntry describes a single file that docforge will assemble into the documentation site.
type SourceEntry struct {
	AssembledPath  string // path in the assembled site (e.g. "docs/other-components/machine-controller-manager/faq")
	RepoURL        string // origin GitHub repo URL (e.g. "https://github.com/gardener/machine-controller-manager")
	RepoFilePath   string // path of the file within the origin repo (e.g. "docs/faq.md")
	RepoLocalClone string // absolute path to the local clone of the repo; empty if not available
	LocalFilePath  string // absolute path to the file on disk (RepoLocalClone + "/" + RepoFilePath)
}

// SourceMap maps assembled site path to its source entry.
type SourceMap map[string]SourceEntry

// ParseManifest reads the root docforge manifest YAML and returns a SourceMap
// containing all remote-sourced files discoverable from local clones.
// reposDir is the directory containing local clones (e.g. ~/Documents/GitHub).
// Warnings are printed for remote repos without a local clone.
func ParseManifest(manifestPath, reposDir string) (SourceMap, error) {
	sm := make(SourceMap)
	if err := parseManifestFile(manifestPath, reposDir, "", sm); err != nil {
		return nil, err
	}
	return sm, nil
}

// --- YAML schema ---

type manifestNode struct {
	Dir          string          `yaml:"dir"`
	File         string          `yaml:"file"`
	FileTree     string          `yaml:"fileTree"`
	Manifest     string          `yaml:"manifest"`
	Source       string          `yaml:"source"`
	ExcludeFiles []string        `yaml:"excludeFiles"`
	Structure    []*manifestNode `yaml:"structure"`
	Frontmatter  interface{}     `yaml:"frontmatter"` // ignored, just parsed to avoid errors
}

type manifestDoc struct {
	Structure []*manifestNode `yaml:"structure"`
}

// --- Parser ---

func parseManifestFile(manifestPath, reposDir, pathPrefix string, sm SourceMap) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("reading manifest %s: %w", manifestPath, err)
	}
	var doc manifestDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parsing manifest %s: %w", manifestPath, err)
	}
	manifestDir := filepath.Dir(manifestPath)
	return processNodes(doc.Structure, reposDir, manifestDir, pathPrefix, sm)
}

func processNodes(nodes []*manifestNode, reposDir, manifestDir, pathPrefix string, sm SourceMap) error {
	for _, node := range nodes {
		if node == nil {
			continue
		}

		// Determine this node's contribution to the assembled path.
		var nodePath string
		switch {
		case node.Dir != "":
			nodePath = joinPath(pathPrefix, node.Dir)
			// Recurse into children.
			if err := processNodes(node.Structure, reposDir, manifestDir, nodePath, sm); err != nil {
				return err
			}

		case node.Manifest != "":
			// Recursive manifest reference — resolve relative to the current manifest's directory.
			ref := node.Manifest
			if !filepath.IsAbs(ref) {
				ref = filepath.Join(manifestDir, ref)
			}
			if err := parseManifestFile(ref, reposDir, pathPrefix, sm); err != nil {
				return err
			}

		case node.File != "":
			nodePath = joinPath(pathPrefix, node.File)
			if node.Source != "" {
				// Remote file with explicit source URL.
				addRemoteFile(node.Source, nodePath, reposDir, sm)
			}
			// Local files (no source) are already in the primary scanned tree — skip.

		case node.FileTree != "":
			if isRemoteURL(node.FileTree) {
				// Remote fileTree — enumerate from local clone.
				addRemoteFileTree(node.FileTree, pathPrefix, node.ExcludeFiles, reposDir, sm)
			}
			// Local fileTree (relative path) — already in the primary scanned tree — skip.
		}
	}
	return nil
}

// addRemoteFile adds a single remote file (from a `source:` URL) to the SourceMap.
func addRemoteFile(sourceURL, assembledPath, reposDir string, sm SourceMap) {
	repoURL, repoFilePath, ok := parseGitHubBlobURL(sourceURL)
	if !ok {
		return
	}
	clonePath := findLocalClone(repoURL, reposDir)
	entry := SourceEntry{
		AssembledPath:  assembledPath,
		RepoURL:        repoURL,
		RepoFilePath:   repoFilePath,
		RepoLocalClone: clonePath,
	}
	if clonePath != "" {
		entry.LocalFilePath = filepath.Join(clonePath, filepath.FromSlash(repoFilePath))
	} else {
		fmt.Fprintf(os.Stderr, "docforge: no local clone found for %s (assembled at %s)\n", repoURL, assembledPath)
	}
	sm[assembledPath] = entry
}

// addRemoteFileTree enumerates all .md/.html files under a remote tree URL and adds each to the SourceMap.
func addRemoteFileTree(treeURL, assembledPathPrefix string, excludeFiles []string, reposDir string, sm SourceMap) {
	repoURL, treePath, ok := parseGitHubTreeURL(treeURL)
	if !ok {
		return
	}
	clonePath := findLocalClone(repoURL, reposDir)
	if clonePath == "" {
		fmt.Fprintf(os.Stderr, "docforge: no local clone found for %s (fileTree: %s)\n", repoURL, treeURL)
		return
	}

	localTreePath := filepath.Join(clonePath, filepath.FromSlash(treePath))
	excludeSet := make(map[string]bool, len(excludeFiles))
	for _, f := range excludeFiles {
		excludeSet[f] = true
	}

	_ = filepath.WalkDir(localTreePath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".html" {
			return nil
		}
		base := filepath.Base(path)
		relToTree, _ := filepath.Rel(localTreePath, path)
		relToTreeSlash := filepath.ToSlash(relToTree)
		// excludeFiles may be a bare filename ("README.md") or a relative path
		// within the tree ("usage/request_cert.md"). Check both.
		if excludeSet[base] || excludeSet[relToTreeSlash] {
			return nil
		}
		repoFilePath := filepath.ToSlash(filepath.Join(treePath, relToTree))

		// Assembled path: strip extension, combine with prefix.
		stem := strings.TrimSuffix(relToTree, filepath.Ext(relToTree))
		assembledPath := joinPath(assembledPathPrefix, filepath.ToSlash(stem))

		sm[assembledPath] = SourceEntry{
			AssembledPath:  assembledPath,
			RepoURL:        repoURL,
			RepoFilePath:   repoFilePath,
			RepoLocalClone: clonePath,
			LocalFilePath:  path,
		}
		return nil
	})
}

// --- URL helpers ---

// parseGitHubBlobURL parses https://github.com/owner/repo/blob/branch/path/to/file
// Returns repoURL (https://github.com/owner/repo), filePath, ok.
func parseGitHubBlobURL(u string) (repoURL, filePath string, ok bool) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(u, prefix) {
		return
	}
	rest := strings.TrimPrefix(u, prefix)
	parts := strings.SplitN(rest, "/", 5)
	if len(parts) < 5 || parts[2] != "blob" {
		return
	}
	repoURL = "https://github.com/" + parts[0] + "/" + parts[1]
	filePath = parts[4]
	ok = true
	return
}

// parseGitHubTreeURL parses https://github.com/owner/repo/tree/branch/path/to/dir
// Returns repoURL, dirPath, ok.
func parseGitHubTreeURL(u string) (repoURL, dirPath string, ok bool) {
	const prefix = "https://github.com/"
	if !strings.HasPrefix(u, prefix) {
		return
	}
	rest := strings.TrimPrefix(u, prefix)
	parts := strings.SplitN(rest, "/", 5)
	if len(parts) < 5 || parts[2] != "tree" {
		return
	}
	repoURL = "https://github.com/" + parts[0] + "/" + parts[1]
	dirPath = parts[4]
	ok = true
	return
}

func isRemoteURL(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}

// findLocalClone checks <reposDir>/<owner>/<repo> and <reposDir>/<repo> for a .git directory.
func findLocalClone(repoURL, reposDir string) string {
	if reposDir == "" {
		return ""
	}
	// Extract owner/repo from URL.
	trimmed := strings.TrimPrefix(repoURL, "https://github.com/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 {
		return ""
	}
	owner, repo := parts[0], parts[1]
	candidates := []string{
		filepath.Join(reposDir, owner, repo),
		filepath.Join(reposDir, repo),
	}
	for _, p := range candidates {
		if _, err := os.Stat(filepath.Join(p, ".git")); err == nil {
			return p
		}
	}
	return ""
}

func joinPath(base, part string) string {
	if base == "" {
		return part
	}
	return base + "/" + part
}
