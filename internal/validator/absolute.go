package validator

import (
	"net/http"
	"strings"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

// ValidateAbsolute checks an absolute URL via HTTP HEAD, falling back to GET on 405.
// client should have an appropriate Timeout set by the caller.
func ValidateAbsolute(link types.Link, client *http.Client, patterns []string) types.ValidationResult {
	if MatchesAnyPattern(link.URL, patterns) {
		return types.ValidationResult{Link: link, Valid: true, Reason: types.ReasonIgnored}
	}

	status, err := headWithFallback(client, link.URL)
	if err != nil {
		if isTimeout(err) {
			return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonTimeout}
		}
		return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonHTTPError}
	}

	if status >= 200 && status < 400 {
		return types.ValidationResult{Link: link, Valid: true, StatusCode: status}
	}
	return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonHTTPError, StatusCode: status}
}

func headWithFallback(client *http.Client, url string) (int, error) {
	resp, err := client.Head(url) //nolint:noctx
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		resp2, err := client.Get(url) //nolint:noctx
		if err != nil {
			return 0, err
		}
		resp2.Body.Close()
		return resp2.StatusCode, nil
	}
	return resp.StatusCode, nil
}

func isTimeout(err error) bool {
	return strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "timeout")
}
