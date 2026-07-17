package main

import (
	"os"
	"strings"
	"testing"
)

// clearGitHubTokenEnv removes all GITHUB_*_TOKEN env vars and GITHUB_TOKEN,
// returning a restore function.
func clearGitHubTokenEnv() func() {
	var saved []struct{ key, val string }
	for _, env := range os.Environ() {
		key, val, ok := strings.Cut(env, "=")
		if !ok {
			continue
		}
		if key == "GITHUB_TOKEN" || (strings.HasPrefix(key, "GITHUB_") && strings.HasSuffix(key, "_TOKEN")) {
			saved = append(saved, struct{ key, val string }{key, val})
			os.Unsetenv(key)
		}
	}
	return func() {
		for _, kv := range saved {
			os.Setenv(kv.key, kv.val)
		}
	}
}

func TestLoadGitHubTokens(t *testing.T) {
	t.Run("GITHUB_TOKEN seeds github.com", func(t *testing.T) {
		restore := clearGitHubTokenEnv()
		defer restore()
		os.Setenv("GITHUB_TOKEN", "tok-public")
		m := loadGitHubTokens(nil)
		if m["github.com"] != "tok-public" {
			t.Errorf("github.com = %q, want tok-public", m["github.com"])
		}
	})

	t.Run("convention discovers GHE host", func(t *testing.T) {
		restore := clearGitHubTokenEnv()
		defer restore()
		os.Setenv("GITHUB_TOOLS_SAP_TOKEN", "tok-ghe")
		m := loadGitHubTokens(nil)
		if m["github.tools.sap"] != "tok-ghe" {
			t.Errorf("github.tools.sap = %q, want tok-ghe", m["github.tools.sap"])
		}
	})

	t.Run("explicit flag overrides convention", func(t *testing.T) {
		restore := clearGitHubTokenEnv()
		defer restore()
		os.Setenv("MY_CUSTOM_TOKEN", "tok-custom")
		defer os.Unsetenv("MY_CUSTOM_TOKEN")
		m := loadGitHubTokens(map[string]string{"github.tools.sap": "MY_CUSTOM_TOKEN"})
		if m["github.tools.sap"] != "tok-custom" {
			t.Errorf("explicit override: github.tools.sap = %q, want tok-custom", m["github.tools.sap"])
		}
	})

	t.Run("empty map when no tokens set", func(t *testing.T) {
		restore := clearGitHubTokenEnv()
		defer restore()
		m := loadGitHubTokens(nil)
		if len(m) != 0 {
			t.Errorf("expected empty map, got %v", m)
		}
	})
}

