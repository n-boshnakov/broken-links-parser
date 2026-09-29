package validator

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestAnchorFragmentNormalisation(t *testing.T) {
	dir := t.TempDir()

	// File with headings that use special characters.
	content := "# Update & Reconcile an Etcd Cluster\n\n## 1.1 Code1\n\n## Conditions\n\n## Assumptions\n"
	targetFile := filepath.Join(dir, "target.md")
	_ = os.WriteFile(targetFile, []byte(content), 0o644)
	sourceFile := filepath.Join(dir, "source.md")
	_ = os.WriteFile(sourceFile, []byte(""), 0o644)

	cases := []struct {
		fragment  string
		wantValid bool
	}{
		// & in fragment should normalise the same way as in the heading.
		{"update-&-reconcile-an-etcd-cluster", true},
		// Dot in fragment should be stripped.
		{"1.1-code1", true},
	}

	for _, tc := range cases {
		link := types.Link{
			URL:        "target.md#" + tc.fragment,
			Type:       types.LinkTypeRelative,
			SourceFile: sourceFile,
		}
		result := ValidateRelative(link, nil, "", "")
		if result.Valid != tc.wantValid {
			t.Errorf("fragment %q: valid=%v, want %v (reason: %s, suggested: %s)",
				tc.fragment, result.Valid, tc.wantValid, result.Reason, result.SuggestedAnchor)
		}
	}
}

func TestExtractAnchors_HTMLIDAttribute(t *testing.T) {
	dir := t.TempDir()
	content := "<h3 id=\"core.gardener.cloud/v1beta1.ClusterAutoscaler\">ClusterAutoscaler\n</h3>\n"
	f := filepath.Join(dir, "api.md")
	_ = os.WriteFile(f, []byte(content), 0o644)

	anchors, err := ExtractAnchors(f)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(anchors, "core.gardener.cloud/v1beta1.ClusterAutoscaler") {
		t.Errorf("raw id not in anchors: %v", anchors)
	}
	if !slices.Contains(anchors, "clusterautoscaler") {
		t.Errorf("normalised text not in anchors: %v", anchors)
	}
}

func TestClosestAnchorPasses(t *testing.T) {
	cases := []struct {
		fragment     string
		candidates   []string
		wantContains string
	}{
		// Pass 3: candidate is substring of fragment (truncated heading).
		{"use-case-3-monitoring-backup-health", []string{"monitoring-backup-health", "other-heading"}, "monitoring-backup-health"},
		// Pass 4: candidate is prefix of fragment.
		{"networkpolicy-controller-registrar", []string{"networkpolicy-controller", "other"}, "networkpolicy-controller"},
		// Pass 1: fragment is prefix of candidate (suffix added like -deprecated).
		{"etcd-components-webhook", []string{"etcd-components-webhook-deprecated", "other"}, "etcd-components-webhook-deprecated"},
		// Pass 5: Levenshtein — single word insertion for long fragment.
		{"ensuring-seeds-capacity-for-shoots-is-not-exceeded", []string{"ensuring-a-seeds-capacity-for-shoots-is-not-exceeded"}, "ensuring-a-seeds-capacity-for-shoots-is-not-exceeded"},
		// Pass 6: word-token overlap — word reorder.
		{"gardener-provided-credentials", []string{"shoot-credentials-gardener-managed", "unrelated"}, "shoot-credentials-gardener-managed"},
		// Pass 6: word-token overlap — leading-dash artefact from normalisation.
		{"-extension-clusterrole--reconciler", []string{"extension-clusterrole-reconciler", "other"}, "extension-clusterrole-reconciler"},
		// Pass 6: word-token overlap — word substitution.
		{"hibernate-a-cluster", []string{"hibernate-your-cluster-manually", "other"}, "hibernate-your-cluster-manually"},
		// Pass 2: short fragment buried in a long candidate is still *selected* (only
		// its score is demoted in Phase 2 — selection identity is unchanged).
		{"garden", []string{"gardener-discovery-server", "other"}, "gardener-discovery-server"},
	}

	for _, tc := range cases {
		got := closestAnchor(tc.fragment, tc.candidates)
		if got != tc.wantContains {
			t.Errorf("closestAnchor(%q) = %q, want %q", tc.fragment, got, tc.wantContains)
		}
	}
}
