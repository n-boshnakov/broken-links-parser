package validator

import (
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

const userAgent = "Mozilla/5.0 (compatible; broken-links-parser/1.0; +https://github.com/n-boshnakov/broken-links-parser)"

// retryableStatus returns true for status codes that warrant a retry.
func retryableStatus(status int) bool {
	return status == 429 || status == 503 || status == 502 || status == 504
}

// ValidateAbsolute checks an absolute URL via HTTP HEAD, falling back to GET on 405.
// Retries up to 2 times with exponential backoff on 429/5xx transient errors.
func ValidateAbsolute(link types.Link, client *http.Client, patterns []string, githubToken string) types.ValidationResult {
	if MatchesAnyPattern(link.URL, patterns) {
		return types.ValidationResult{Link: link, Valid: true, Reason: types.ReasonIgnored}
	}

	if !strings.HasPrefix(link.URL, "http://") && !strings.HasPrefix(link.URL, "https://") {
		return types.ValidationResult{Link: link, Valid: true, Reason: types.ReasonIgnored}
	}

	var status int
	var err error
	for attempt := 0; attempt <= 2; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1))*time.Second +
				time.Duration(rand.Intn(500))*time.Millisecond
			time.Sleep(backoff)
		}
		status, err = headWithFallback(client, link.URL, githubToken)
		if err != nil {
			if isTimeout(err) {
				return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonTimeout}
			}
			return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonHTTPError}
		}
		if !retryableStatus(status) {
			break
		}
	}

	if status >= 200 && status < 400 {
		return types.ValidationResult{Link: link, Valid: true, StatusCode: status}
	}
	return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonHTTPError, StatusCode: status}
}

// CheckURL performs a HEAD→GET check on a raw URL string and returns true if reachable.
func CheckURL(rawURL string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	status, err := headWithFallback(client, rawURL, "")
	return err == nil && status >= 200 && status < 400
}

func headWithFallback(client *http.Client, url, githubToken string) (int, error) {
	resp, err := doRequest(client, http.MethodHead, url, githubToken)
	if err != nil {
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
