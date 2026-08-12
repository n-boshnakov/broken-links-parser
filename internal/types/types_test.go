package types

import "testing"

func TestConfidenceLabel(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{1.0, ConfidenceHigh},
		{0.95, ConfidenceHigh}, // git-strategy score must still label high
		{0.8, ConfidenceHigh},  // boundary
		{0.79, ConfidenceMedium},
		{0.7, ConfidenceMedium},
		{0.5, ConfidenceMedium}, // boundary
		{0.49, ConfidenceLow},
		{0.0, ConfidenceLow},
	}
	for _, tc := range cases {
		if got := ConfidenceLabel(tc.score); got != tc.want {
			t.Errorf("ConfidenceLabel(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestIsBroken(t *testing.T) {
	cases := []struct {
		name string
		r    ValidationResult
		want bool
	}{
		{"valid", ValidationResult{Valid: true}, false},
		{"ignored", ValidationResult{Valid: false, Reason: ReasonIgnored}, false},
		{"auth-blocked is not broken", ValidationResult{Valid: false, Reason: ReasonAuthBlocked}, false},
		{"network-error is not broken", ValidationResult{Valid: false, Reason: ReasonNetworkError}, false},
		{"http-error is broken", ValidationResult{Valid: false, Reason: ReasonHTTPError}, true},
		{"file-not-found is broken", ValidationResult{Valid: false, Reason: ReasonFileNotFound}, true},
		{"anchor-not-found is broken", ValidationResult{Valid: false, Reason: ReasonAnchorNotFound}, true},
	}
	for _, tc := range cases {
		if got := tc.r.IsBroken(); got != tc.want {
			t.Errorf("%s: IsBroken() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
