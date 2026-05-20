// Package youtube resolves YouTube channel/handle URLs to Atom feeds.
package youtube

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	feedURLBase  = "https://www.youtube.com/feeds/videos.xml?channel_id="
	fetchTimeout = 10 * time.Second
)

var (
	reMetaChannel = regexp.MustCompile(`<meta\s+itemprop="(?:channelId|identifier)"\s+content="(UC[^"]+)"`)

	client = &http.Client{Timeout: fetchTimeout}
)

// ResolveURL converts a YouTube channel/handle URL to its Atom feed URL;
// other URLs pass through.
func ResolveURL(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return raw, nil //nolint:nilerr // pass-through on parse failure
	}
	if !isYouTubeHost(u.Host) {
		return raw, nil
	}
	if strings.HasPrefix(u.Path, "/feeds/videos.xml") {
		return raw, nil
	}
	if id, ok := strings.CutPrefix(u.Path, "/channel/"); ok {
		id, _, _ = strings.Cut(id, "/")
		if strings.HasPrefix(id, "UC") {
			return feedURLBase + id, nil
		}
	}
	if !isHandlePath(u.Path) {
		return raw, nil
	}

	body, err := fetchChannelPage(ctx, raw)
	if err != nil {
		return "", err
	}
	if id, ok := parseChannelID(body); ok {
		return feedURLBase + id, nil
	}
	return "", errors.New("channel ID not found in page")
}

// IsShort reports whether an entry URL is a YouTube Shorts video.
func IsShort(link string) bool {
	return strings.Contains(link, "youtube.com/shorts/")
}

func isYouTubeHost(host string) bool {
	host = strings.TrimPrefix(host, "www.")
	host = strings.TrimPrefix(host, "m.")
	return host == "youtube.com"
}

func isHandlePath(path string) bool {
	return strings.HasPrefix(path, "/@") ||
		strings.HasPrefix(path, "/c/") ||
		strings.HasPrefix(path, "/user/")
}

func parseChannelID(body []byte) (string, bool) {
	if m := reMetaChannel.FindSubmatch(body); m != nil {
		return string(m[1]), true
	}
	return "", false
}

func fetchChannelPage(ctx context.Context, pageURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (rss2tg)")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching channel page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching channel page: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading channel page: %w", err)
	}
	return body, nil
}
