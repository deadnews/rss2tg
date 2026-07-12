// Package github rewrites GitHub repo URLs to their Atom feeds.
package github

import (
	"net/url"
	"path"
	"strings"
)

// FeedURL rewrites a GitHub repo, releases, or tags URL to its Atom feed;
// a bare repo URL defaults to its releases feed.
func FeedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Host, "github.com") {
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
