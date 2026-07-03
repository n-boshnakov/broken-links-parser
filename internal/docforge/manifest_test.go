package docforge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseManifest_SingleFileWithSource(t *testing.T) {
	sm, err := ParseManifest("testdata/root.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := sm["guides/ha-best-practices"]
	if !ok {
		t.Fatal("expected entry for guides/ha-best-practices")
	}
	if entry.RepoURL != "https://github.com/gardener/gardener" {
		t.Errorf("RepoURL = %q", entry.RepoURL)
	}
	if entry.RepoFilePath != "docs/usage/ha.md" {
		t.Errorf("RepoFilePath = %q", entry.RepoFilePath)
	}
	if entry.RepoLocalClone != "" {
		t.Error("expected empty RepoLocalClone when no reposDir")
	}
}

func TestParseManifest_RecursiveManifest(t *testing.T) {
	sm, err := ParseManifest("testdata/root.yaml", "")
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := sm["other/etcd-druid"]
	if !ok {
		t.Fatal("expected entry for other/etcd-druid from sub-manifest")
	}
	if entry.RepoURL != "https://github.com/gardener/etcd-druid" {
		t.Errorf("RepoURL = %q", entry.RepoURL)
	}
}

func TestParseManifest_FileTreeWithExcludeFiles(t *testing.T) {
	// Set up a fake local clone for machine-controller-manager.
	reposDir := t.TempDir()
	cloneDir := filepath.Join(reposDir, "gardener", "machine-controller-manager")
	docsDir := filepath.Join(cloneDir, "docs")
	_ = os.MkdirAll(filepath.Join(docsDir, ".git"), 0o755) // .git in parent
	_ = os.MkdirAll(docsDir, 0o755)
	// Simulate .git at clone root
	_ = os.MkdirAll(filepath.Join(cloneDir, ".git"), 0o755)
	_ = os.WriteFile(filepath.Join(docsDir, "concepts.md"), []byte("# Concepts"), 0o644)
	_ = os.WriteFile(filepath.Join(docsDir, "README.md"), []byte("# README"), 0o644)

	sm, err := ParseManifest("testdata/root.yaml", reposDir)
	if err != nil {
		t.Fatal(err)
	}

	// concepts.md should be included.
	if _, ok := sm["guides/concepts"]; !ok {
		t.Error("expected guides/concepts in SourceMap")
	}
	// README.md should be excluded.
	for k := range sm {
		if k == "guides/README" || k == "guides/readme" {
			t.Errorf("README should have been excluded, found key %q", k)
		}
	}
}

func TestParseManifest_MissingLocalClone_NoError(t *testing.T) {
	// No reposDir set — should produce entries with empty LocalFilePath, no error.
	sm, err := ParseManifest("testdata/root.yaml", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	entry := sm["guides/ha-best-practices"]
	if entry.LocalFilePath != "" {
		t.Errorf("expected empty LocalFilePath, got %q", entry.LocalFilePath)
	}
}
