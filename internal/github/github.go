// Package github rewrites GitHub repo URLs to their Atom feeds.
package github

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	host         = "github.com"
	fetchTimeout = 10 * time.Second
)

var client = &http.Client{
	Timeout:       fetchTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// FeedURL rewrites a GitHub repo, releases, or tags URL to its Atom feed;
// a bare repo URL defaults to its releases feed.
func FeedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, host) {
		return raw
	}
	if strings.HasSuffix(u.Path, ".atom") {
		return raw
	}

	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(segs) == 2:
		u.Path = "/" + path.Join(segs[0], segs[1], "releases.atom")
	case len(segs) == 3 && (segs[2] == "releases" || segs[2] == "tags"):
		u.Path += ".atom"
	default:
		return raw
	}
	return u.String()
}

// LatestURL reports whether feedURL is a releases feed,
// returning the repo URL that redirects to its latest release.
func LatestURL(feedURL string) (string, bool) {
	u, err := url.Parse(feedURL)
	if err != nil || !strings.EqualFold(u.Host, host) {
		return "", false
	}

	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(segs) != 3 || segs[2] != "releases.atom" {
		return "", false
	}
	u.Path = "/" + path.Join(segs[0], segs[1], "releases", "latest")
	return u.String(), true
}

// ResolveLatest returns the release page feedURL redirects to,
// matching the Atom entry link; empty when no release is labeled latest.
func ResolveLatest(ctx context.Context, feedURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, feedURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode < 300 || resp.StatusCode > 399 {
		return "", fmt.Errorf("fetch latest release: status %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.Contains(loc, "/releases/tag/") {
		return "", nil
	}
	return loc, nil
}
