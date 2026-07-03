package pipeline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtract_WithDocforgeManifest(t *testing.T) {
	// Set up a fake local clone with one markdown file containing a link.
	reposDir := t.TempDir()
	cloneDir := filepath.Join(reposDir, "gardener", "myrepo")
	docsDir := filepath.Join(cloneDir, "docs")
	_ = os.MkdirAll(filepath.Join(cloneDir, ".git"), 0o755)
	_ = os.MkdirAll(docsDir, 0o755)
	_ = os.WriteFile(filepath.Join(docsDir, "guide.md"),
		[]byte("[other](../other.md)\n"), 0o644)

	// Write a minimal manifest referencing the fake clone.
	manifestDir := t.TempDir()
	manifestPath := filepath.Join(manifestDir, "manifest.yaml")
	manifestContent := `structure:
- dir: docs
  structure:
  - file: guide
    source: https://github.com/gardener/myrepo/blob/master/docs/guide.md
`
	_ = os.WriteFile(manifestPath, []byte(manifestContent), 0o644)

	// Also need a local root to scan (can be empty).
	localRoot := t.TempDir()

	links, sm, err := Extract(Options{
		Root:             localRoot,
		DocforgeManifest: manifestPath,
		ReposDir:         reposDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sm == nil {
		t.Fatal("expected non-nil SourceMap")
	}
	if _, ok := sm["docs/guide"]; !ok {
		t.Error("expected docs/guide in SourceMap")
	}

	// Find the extracted link from the sourced file.
	var found bool
	for _, l := range links {
		if l.SourceRepo != "" {
			found = true
			if l.SourceRepo != cloneDir {
				t.Errorf("SourceRepo = %q, want %q", l.SourceRepo, cloneDir)
			}
		}
	}
	if !found {
		t.Error("expected at least one link with SourceRepo set from sourced file")
	}
}
