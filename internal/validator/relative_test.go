package validator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

func TestNormaliseAnchor(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Installation", "installation"},
		{"Getting Started", "getting-started"},
		{"foo & bar", "foo--bar"}, // & stripped, surrounding spaces each become -, giving --
		{"  spaces  ", "spaces"},
	}
	for _, c := range cases {
		if got := NormaliseAnchor(c.in); got != c.want {
			t.Errorf("NormaliseAnchor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestValidateRelative(t *testing.T) {
	root := t.TempDir()
	write := func(rel, content string) string {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(content), 0o644)
		return p
	}

	srcFile := write("docs/guide.md", "# Installation\n\nSome text.\n")
	write("docs/other.md", "# Overview\n")

	link := func(url string, lt types.LinkType) types.Link {
		return types.Link{URL: url, Type: lt, SourceFile: srcFile}
	}

	tests := []struct {
		name     string
		link     types.Link
		patterns []string
		wantOK   bool
		wantReason string
	}{
		{"valid relative file", link("other.md", types.LinkTypeRelative), nil, true, ""},
		{"missing file", link("missing.md", types.LinkTypeRelative), nil, false, types.ReasonFileNotFound},
		{"valid anchor", link("guide.md#installation", types.LinkTypeRelative), nil, true, ""},
		{"missing anchor", link("guide.md#no-such-heading", types.LinkTypeRelative), nil, false, types.ReasonAnchorNotFound},
		{"same-file anchor valid", link("#installation", types.LinkTypeAnchor), nil, true, ""},
		{"same-file anchor missing", link("#nope", types.LinkTypeAnchor), nil, false, types.ReasonAnchorNotFound},
		{"ignored by pattern", link("missing.md", types.LinkTypeRelative), []string{"missing*"}, true, types.ReasonIgnored},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateRelative(tc.link, tc.patterns, "", "")
			if got.Valid != tc.wantOK {
				t.Errorf("Valid: got %v, want %v", got.Valid, tc.wantOK)
			}
			if got.Reason != tc.wantReason {
				t.Errorf("Reason: got %q, want %q", got.Reason, tc.wantReason)
			}
		})
	}
}

func TestValidateRelative_RootRelativeBase(t *testing.T) {
	// Layout: <repo>/hugo/content/{page.md, adopter/img.svg}
	repo := t.TempDir()
	base := filepath.Join(repo, "hugo", "content")
	_ = os.MkdirAll(filepath.Join(base, "adopter"), 0o755)
	src := filepath.Join(base, "page.md")
	_ = os.WriteFile(src, []byte("# Page\n"), 0o644)
	_ = os.WriteFile(filepath.Join(base, "adopter", "img.svg"), []byte("<svg/>"), 0o644)

	rootLink := types.Link{URL: "/adopter/img.svg", Type: types.LinkTypeImage, SourceFile: src}

	t.Run("resolves against content base when set", func(t *testing.T) {
		got := ValidateRelative(rootLink, nil, repo, base)
		if !got.Valid {
			t.Errorf("expected valid, got reason=%q", got.Reason)
		}
	})

	t.Run("missing under base is FILE_NOT_FOUND", func(t *testing.T) {
		missing := types.Link{URL: "/adopter/nope.svg", Type: types.LinkTypeImage, SourceFile: src}
		got := ValidateRelative(missing, nil, repo, base)
		if got.Valid || got.Reason != types.ReasonFileNotFound {
			t.Errorf("expected FILE_NOT_FOUND, got valid=%v reason=%q", got.Valid, got.Reason)
		}
	})

	t.Run("without base falls back to repo root (original behaviour)", func(t *testing.T) {
		// /adopter/img.svg joined onto repo root does NOT exist (it's under hugo/content).
		got := ValidateRelative(rootLink, nil, repo, "")
		if got.Valid {
			t.Error("expected not valid when base unset (repo-root resolution misses)")
		}
	})

	t.Run("relative ./ and ../ links are unaffected by base", func(t *testing.T) {
		// ../ from page.md reaches <repo>/hugo/ — put a sibling there.
		_ = os.WriteFile(filepath.Join(repo, "hugo", "sibling.md"), []byte("x"), 0o644)
		rel := types.Link{URL: "../sibling.md", Type: types.LinkTypeRelative, SourceFile: src}
		got := ValidateRelative(rel, nil, repo, base)
		if !got.Valid {
			t.Errorf("relative link should resolve regardless of base, got reason=%q", got.Reason)
		}
	})
}

func TestValidateRelative_RouteFallback(t *testing.T) {
	base := t.TempDir()
	src := filepath.Join(base, "page.md")
	_ = os.WriteFile(src, []byte("# Page\n"), 0o644)
	// Route /docs/foo/ → foo.md (file, no dir).
	_ = os.MkdirAll(filepath.Join(base, "docs"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "docs", "foo.md"), []byte("# Foo\n## Deep Section\n"), 0o644)
	// Route /guides/ → guides/_index.md (section index).
	_ = os.MkdirAll(filepath.Join(base, "guides"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "guides", "_index.md"), []byte("# Guides\n"), 0o644)
	// Route /api/ → api/index.md (leaf bundle).
	_ = os.MkdirAll(filepath.Join(base, "api"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "api", "index.md"), []byte("# API\n"), 0o644)
	// Route with dots in the final segment → file with a "fake extension".
	_ = os.MkdirAll(filepath.Join(base, "blog", "2018"), 0o755)
	_ = os.WriteFile(filepath.Join(base, "blog", "2018", "12.25-cookies.md"), []byte("# Cookies\n"), 0o644)

	link := func(url string) types.Link {
		return types.Link{URL: url, Type: types.LinkTypeRelative, SourceFile: src}
	}

	cases := []struct {
		name   string
		url    string
		valid  bool
		reason string
	}{
		{"trailing-slash route to .md file", "/docs/foo/", true, ""},
		{"extensionless route to .md file", "/docs/foo", true, ""},
		{"route to _index.md section", "/guides/", true, ""},
		{"route to index.md bundle", "/api/", true, ""},
		{"dotted-slug route to .md file", "/blog/2018/12.25-cookies/", true, ""},
		{"route + valid anchor into resolved file", "/docs/foo/#deep-section", true, ""},
		{"route + missing anchor", "/docs/foo/#nope", false, types.ReasonAnchorNotFound},
		{"genuinely missing route", "/docs/ghost/", false, types.ReasonFileNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateRelative(link(tc.url), nil, base, base)
			if got.Valid != tc.valid {
				t.Errorf("Valid = %v, want %v (reason=%q)", got.Valid, tc.valid, got.Reason)
			}
			if got.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.reason)
			}
		})
	}
}

func TestValidateRelative_DirectoryTarget(t *testing.T) {
	repo := t.TempDir()
	// A source-code package directory containing no markdown/index — a link to it
	// is valid on GitHub (renders the directory listing).
	pkgDir := filepath.Join(repo, "pkg", "controller", "managedresource")
	_ = os.MkdirAll(pkgDir, 0o755)
	_ = os.WriteFile(filepath.Join(pkgDir, "controller.go"), []byte("package x\n"), 0o644)
	// A directory that DOES have an index → should resolve to that file.
	docDir := filepath.Join(repo, "docs", "topic")
	_ = os.MkdirAll(docDir, 0o755)
	_ = os.WriteFile(filepath.Join(docDir, "index.md"), []byte("# Topic\n## Section\n"), 0o644)

	src := filepath.Join(repo, "docs", "guide.md")
	_ = os.WriteFile(src, []byte("# Guide\n"), 0o644)
	link := func(url string, lt types.LinkType) types.Link {
		return types.Link{URL: url, Type: lt, SourceFile: src}
	}

	cases := []struct {
		name   string
		link   types.Link
		valid  bool
		reason string
	}{
		{"relative link to code directory (no index)", link("../pkg/controller/managedresource", types.LinkTypeRelative), true, ""},
		{"relative link to directory with index", link("topic", types.LinkTypeRelative), true, ""},
		{"anchor into directory-with-index resolves heading", link("topic#section", types.LinkTypeRelative), true, ""},
		{"link to nonexistent directory", link("../pkg/controller/ghost", types.LinkTypeRelative), false, types.ReasonFileNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateRelative(tc.link, nil, repo, "")
			if got.Valid != tc.valid {
				t.Errorf("Valid = %v, want %v (reason=%q)", got.Valid, tc.valid, got.Reason)
			}
			if got.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q", got.Reason, tc.reason)
			}
		})
	}
}

func TestClosestAnchorScored(t *testing.T) {
	cases := []struct {
		name       string
		fragment   string
		candidates []string
		wantAnchor string
		minScore   float64
		maxScore   float64
	}{
		{"prefix match", "etcd-components-webhook", []string{"etcd-components-webhook-deprecated", "other"}, "etcd-components-webhook-deprecated", 0.9, 0.9},
		{"reverse-substring wins over reverse-prefix (candidate contained in fragment)", "networkpolicy-controller-registrar", []string{"networkpolicy-controller", "other"}, "networkpolicy-controller", 0.6, 0.6},
		{"reverse-substring match", "use-case-3-monitoring-backup-health", []string{"monitoring-backup-health", "x"}, "monitoring-backup-health", 0.6, 0.6},
		{"token-overlap match", "gardener-provided-credentials", []string{"shoot-credentials-gardener-managed", "unrelated"}, "shoot-credentials-gardener-managed", 0.7, 0.9},
		{"no match", "totally-unrelated-xyz", []string{"something-else-entirely"}, "", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotAnchor, gotScore := closestAnchorScored(tc.fragment, tc.candidates)
			if gotAnchor != tc.wantAnchor {
				t.Errorf("anchor = %q, want %q", gotAnchor, tc.wantAnchor)
			}
			if gotScore < tc.minScore || gotScore > tc.maxScore {
				t.Errorf("score = %v, want in [%v, %v]", gotScore, tc.minScore, tc.maxScore)
			}
		})
	}
	// The string-only wrapper must agree with the scored variant's anchor.
	if s := closestAnchor("etcd-components-webhook", []string{"etcd-components-webhook-deprecated"}); s != "etcd-components-webhook-deprecated" {
		t.Errorf("closestAnchor wrapper = %q", s)
	}
}

func TestValidateRelative_SuggestedAnchorScoreSet(t *testing.T) {
	dir := t.TempDir()
	// Target file with a heading that a fuzzy fragment should match.
	target := filepath.Join(dir, "guide.md")
	_ = os.WriteFile(target, []byte("# Installation Steps\n"), 0o644)
	src := filepath.Join(dir, "src.md")
	_ = os.WriteFile(src, []byte(""), 0o644)

	t.Run("fuzzy anchor sets a positive score", func(t *testing.T) {
		link := types.Link{URL: "guide.md#installation-step", Type: types.LinkTypeRelative, SourceFile: src}
		res := ValidateRelative(link, nil, "", "")
		if res.Reason != types.ReasonAnchorNotFound {
			t.Fatalf("Reason = %q, want ANCHOR_NOT_FOUND", res.Reason)
		}
		if res.SuggestedAnchor == "" || res.SuggestedAnchorScore <= 0 {
			t.Errorf("expected a scored suggestion, got anchor=%q score=%v", res.SuggestedAnchor, res.SuggestedAnchorScore)
		}
	})

	t.Run("no candidate leaves score zero", func(t *testing.T) {
		empty := filepath.Join(dir, "empty.md")
		_ = os.WriteFile(empty, []byte("no headings here\n"), 0o644)
		link := types.Link{URL: "empty.md#nonexistent-heading", Type: types.LinkTypeRelative, SourceFile: src}
		res := ValidateRelative(link, nil, "", "")
		if res.Reason != types.ReasonAnchorNotFound {
			t.Fatalf("Reason = %q, want ANCHOR_NOT_FOUND", res.Reason)
		}
		if res.SuggestedAnchorScore != 0 {
			t.Errorf("expected score 0 with no candidates, got %v", res.SuggestedAnchorScore)
		}
	})
}
