package validator

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/n-boshnakov/broken-links-parser/internal/types"
)

const userAgent = "Mozilla/5.0 (compatible; broken-links-parser/1.0; +https://github.com/n-boshnakov/broken-links-parser)"

// warnedHosts tracks GHE hosts already warned about missing tokens (one warning per host per run).
var warnedHosts sync.Map

// tokenForURL returns the GitHub token for the given URL's host, or "" if none configured.
func tokenForURL(rawURL string, tokens map[string]string) string {
	if len(tokens) == 0 {
		return ""
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname() // strips port
	tok := tokens[host]
	// Warn once per non-github.com host that has no token.
	if tok == "" && host != "github.com" && host != "" && isKnownGHEPattern(host) {
		if _, already := warnedHosts.LoadOrStore(host, true); !already {
			fmt.Fprintf(os.Stderr, "Warning: no token configured for %s — requests will be unauthenticated\n", host)
		}
	}
	return tok
}

// isKnownGHEPattern returns true for hosts that look like GitHub Enterprise instances
// (i.e. "github." prefix or any host present as a key in the token map).
// Used only to decide whether to emit the unauthenticated-host warning.
func isKnownGHEPattern(host string) bool {
	return strings.HasPrefix(host, "github.")
}

// retryableStatus returns true for status codes that warrant a retry.
func retryableStatus(status int) bool {
	return status == 429 || status == 503 || status == 502 || status == 504
}

// ValidateAbsolute checks an absolute URL via HTTP HEAD, falling back to GET when
// the server rejects HEAD (405/501/403). Retries up to 2 times with exponential
// backoff on 429/5xx transient errors.
//
// Classification:
//   - 200–399           → valid
//   - 401/403           → AUTH_BLOCKED (page likely exists but rejects automated access)
//   - other 4xx/5xx     → HTTP_ERROR
//   - timeout           → TIMEOUT
//   - DNS/refused/TLS   → NETWORK_ERROR
func ValidateAbsolute(link types.Link, client *http.Client, patterns []string, tokens map[string]string) types.ValidationResult {
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
		status, err = headWithFallback(client, link.URL, tokens)
		if err != nil {
			if isTimeout(err) {
				return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonTimeout}
			}
			if isNetworkError(err) {
				return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonNetworkError}
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
	// 401/403 almost always mean the page exists but blocks automated/unauthenticated
	// access; surface as a distinct, non-broken state rather than HTTP_ERROR.
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonAuthBlocked, StatusCode: status}
	}
	return types.ValidationResult{Link: link, Valid: false, Reason: types.ReasonHTTPError, StatusCode: status}
}

// CheckURL performs a HEAD→GET check on a raw URL string and returns true if reachable.
func CheckURL(rawURL string) bool {
	client := &http.Client{Timeout: 10 * time.Second}
	status, err := headWithFallback(client, rawURL, nil)
	return err == nil && status >= 200 && status < 400
}

func headWithFallback(client *http.Client, url string, tokens map[string]string) (int, error) {
	resp, err := doRequest(client, http.MethodHead, url, tokens)
	if err != nil {
		if strings.Contains(err.Error(), "received DATA on a HEAD request") {
			resp2, err2 := doRequest(client, http.MethodGet, url, tokens)
			if err2 != nil {
				return 0, err2
			}
			resp2.Body.Close()
			return resp2.StatusCode, nil
		}
		return 0, err
	}
	resp.Body.Close()
	// Some servers reject HEAD (405/501) or gate it behind auth handling that a GET
	// clears (403); retry those with GET before trusting the status.
	if resp.StatusCode == http.StatusMethodNotAllowed ||
		resp.StatusCode == http.StatusNotImplemented ||
		resp.StatusCode == http.StatusForbidden {
		resp2, err := doRequest(client, http.MethodGet, url, tokens)
		if err != nil {
			return 0, err
		}
		resp2.Body.Close()
		return resp2.StatusCode, nil
	}
	return resp.StatusCode, nil
}

func doRequest(client *http.Client, method, rawURL string, tokens map[string]string) (*http.Response, error) {
	req, err := http.NewRequest(method, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if tok := tokenForURL(rawURL, tokens); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	return client.Do(req)
}

func isTimeout(err error) bool {
	return strings.Contains(err.Error(), "context deadline exceeded") ||
		strings.Contains(err.Error(), "timeout")
}

// isNetworkError reports whether err is a transport-level failure (DNS resolution,
// connection refused, or TLS/certificate) rather than an HTTP-status outcome.
func isNetworkError(err error) bool {
	msg := err.Error()
	for _, s := range []string{
		"no such host",
		"server misbehaving",
		"connection refused",
		"connection reset",
		"network is unreachable",
		"no route to host",
		"tls:",
		"x509",
		"certificate",
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

