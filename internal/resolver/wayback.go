package resolver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// WaybackContext holds optional enrichment context for the AI prompt.
type WaybackContext struct {
	SnapshotURL string // specific timestamped Wayback URL, empty if not found
	Title       string // page title from the archived snapshot
	Excerpt     string // first ~500 chars of body text from the archived snapshot
	ParentURL   string // root origin URL if alive, empty otherwise
}

// FetchWaybackSnapshot queries the Wayback Machine CDX API for the closest snapshot
// of rawURL, fetches it, and extracts the page title and a short text excerpt.
// Returns found=false on timeout, no snapshot, or archived 4xx/5xx status.
func FetchWaybackSnapshot(rawURL string) (snapshotURL, title, excerpt string, found bool) {
	cdxURL := "https://archive.org/wayback/available?url=" + rawURL
	client := &http.Client{Timeout: 5 * time.Second}
	return fetchWaybackWithCDX(cdxURL, client)
}

// fetchWaybackWithCDX is the testable core: accepts any CDX URL and HTTP client.
func fetchWaybackWithCDX(cdxURL string, client *http.Client) (snapshotURL, title, excerpt string, found bool) {
	snap, status, ok := queryCDX(cdxURL, client)
	if !ok || snap == "" {
		return
	}
	// Reject snapshots that were themselves error pages.
	if len(status) > 0 && (status[0] == '4' || status[0] == '5') {
		return
	}
	snapshotURL = snap
	title, excerpt = fetchSnapshotContent(snapshotURL)
	found = true
	return
}

// fetchWaybackWithCDXClient is an alias used in tests to inject a timeout-controlled client.
func fetchWaybackWithCDXClient(cdxURL string, cdxClient *http.Client) (snapshotURL, title, excerpt string, found bool) {
	return fetchWaybackWithCDX(cdxURL, cdxClient)
}

// queryCDX calls the CDX API and returns the closest snapshot URL, its archived status code, and whether one was found.
func queryCDX(cdxURL string, client *http.Client) (snapshotURL, status string, found bool) {
	resp, err := client.Get(cdxURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var cdx struct {
		ArchivedSnapshots struct {
			Closest struct {
				Available bool   `json:"available"`
				URL       string `json:"url"`
				Status    string `json:"status"`
			} `json:"closest"`
		} `json:"archived_snapshots"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&cdx); err != nil {
		return
	}
	closest := cdx.ArchivedSnapshots.Closest
	if !closest.Available || closest.URL == "" {
		return
	}
	return closest.URL, closest.Status, true
}

// fetchSnapshotContent fetches a Wayback snapshot and extracts title + text excerpt (~500 chars).
func fetchSnapshotContent(snapshotURL string) (title, excerpt string) {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(snapshotURL)
	if err != nil || resp.StatusCode >= 400 {
		return
	}
	defer resp.Body.Close()

	var sb strings.Builder
	inTitle := false
	inContent := false
	depth := 0

	z := html.NewTokenizer(resp.Body)
	for sb.Len() < 500 {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()

		switch tt {
		case html.StartTagToken:
			tag := tok.Data
			if tag == "title" {
				inTitle = true
			}
			if tag == "h1" || tag == "h2" || tag == "h3" || tag == "p" {
				inContent = true
				depth++
			}
		case html.EndTagToken:
			tag := tok.Data
			if tag == "title" {
				inTitle = false
			}
			if tag == "h1" || tag == "h2" || tag == "h3" || tag == "p" {
				depth--
				if depth <= 0 {
					inContent = false
					depth = 0
					sb.WriteByte('\n')
				}
			}
		case html.TextToken:
			text := strings.TrimSpace(tok.Data)
			if text == "" {
				continue
			}
			if inTitle && title == "" {
				title = text
			}
			if inContent {
				if sb.Len() > 0 {
					sb.WriteByte(' ')
				}
				remaining := 500 - sb.Len()
				if len(text) > remaining {
					text = text[:remaining]
				}
				sb.WriteString(text)
			}
		}
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	excerpt = strings.TrimSpace(sb.String())
	return
}

// checkParentSite checks whether the root origin (scheme+host) of rawURL is alive.
// cache maps origin → liveness; pass nil to disable caching.
func checkParentSite(rawURL string, cache map[string]bool) (parentURL string, live bool) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return
	}
	// Skip GitHub — those links go through the git resolver, not AI.
	if strings.HasSuffix(u.Host, "github.com") {
		return
	}
	origin := u.Scheme + "://" + u.Host
	// Skip if the origin is the same as the full URL (no path to strip).
	if strings.TrimRight(rawURL, "/") == strings.TrimRight(origin, "/") {
		return
	}
	// Check cache.
	if cache != nil {
		if cached, ok := cache[origin]; ok {
			if cached {
				return origin, true
			}
			return
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Head(origin)
	if err != nil {
		if cache != nil {
			cache[origin] = false
		}
		return
	}
	resp.Body.Close()
	isLive := resp.StatusCode >= 200 && resp.StatusCode < 400
	if cache != nil {
		cache[origin] = isLive
	}
	if isLive {
		return origin, true
	}
	return
}
