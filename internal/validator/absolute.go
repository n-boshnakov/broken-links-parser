package validator

import (
	"net/http"
	"strings"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

const userAgent = "Mozilla/5.0 (compatible; broken-links-parser/1.0; +https://github.com/n-boshnakov/broken-links-parser)"

// ValidateAbsolute checks an absolute URL via HTTP HEAD, falling back to GET on 405.
// client should have an appropriate Timeout set by the caller.
func ValidateAbsolute(link types.Link, client *http.Client, patterns []string, githubToken string) types.ValidationResult {
	if MatchesAnyPattern(link.URL, patterns) {
		return types.ValidationResult{Link: link, Valid: true, Reason: types.ReasonIgnored}
	}

	// Non-HTTP schemes (mailto:, tel:, ftp:, etc.) cannot be checked via HTTP.
	// If not explicitly ignored above, mark as ignored rather than erroring.
	if !strings.HasPrefix(link.URL, "http://") && !strings.HasPrefix(link.URL, "https://") {
		return types.ValidationResult{Link: link, Valid: true, Reason: types.ReasonIgnored}
	}

	status, err := headWithFallback(client, link.URL, githubToken)
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

// CheckURL performs a HEAD→GET check on a raw URL string and returns true if reachable.
// Timeouts and non-2xx/3xx responses return false. Used by the AI resolver to validate candidates.
func CheckURL(rawURL string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	status, err := headWithFallback(client, rawURL, "")
	return err == nil && status >= 200 && status < 400
}

func headWithFallback(client *http.Client, url, githubToken string) (int, error) {
	resp, err := doRequest(client, http.MethodHead, url, githubToken)
	if err != nil {
		// Some servers violate HTTP/2 by sending a body on a HEAD response.
		// Go's http2 stack rejects this with "received DATA on a HEAD request".
		// Fall back to GET, same as we do for 405.
		if strings.Contains(err.Error(), "received DATA on a HEAD request") {
			resp2, err2 := doRequest(client, http.MethodGet, url, githubToken)
			if err2 != nil {
				return 0, err2
			}
			resp2.Body.Close()
			return resp2.StatusCode, nil
		}
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusMethodNotAllowed {
		resp2, err := doRequest(client, http.MethodGet, url, githubToken)
		if err != nil {
			return 0, err
		}
		resp2.Body.Close()
		return resp2.StatusCode, nil
	}
	return resp.StatusCode, nil
}

func doRequest(client *http.Client, method, url, githubToken string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if githubToken != "" && isGitHubURL(url) {
		req.Header.Set("Authorization", "Bearer "+githubToken)
	}
	return client.Do(req)
}

func isGitHubURL(url string) bool {
	return strings.HasPrefix(url, "https://github.com/") ||
		strings.HasPrefix(url, "https://api.github.com/") ||
		strings.HasPrefix(url, "https://raw.githubusercontent.com/")
}

func isTimeout(err error) bool {
	return strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "timeout")
}
