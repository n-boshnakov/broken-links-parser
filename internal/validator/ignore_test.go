package validator

import "testing"

func TestMatchesAnyPattern(t *testing.T) {
	tests := []struct {
		url      string
		patterns []string
		want     bool
	}{
		{"https://internal.example.com/page", []string{"https://internal.*"}, true},
		{"https://github.com/org/repo", []string{"https://internal.*"}, false},
		{"https://a.com", []string{"https://b.*", "https://a.*"}, true},  // second matches
		{"https://a.com", []string{}, false},                              // empty list
		{"https://a.com", nil, false},                                     // nil list
		{"https://exact.com", []string{"https://exact.com"}, true},        // exact match
	}
	for _, tc := range tests {
		got := MatchesAnyPattern(tc.url, tc.patterns)
		if got != tc.want {
			t.Errorf("MatchesAnyPattern(%q, %v) = %v, want %v", tc.url, tc.patterns, got, tc.want)
		}
	}
}
