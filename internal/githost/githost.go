// Package githost rewrites git-host repo URLs to their Atom feeds.
package githost

import (
	"net/url"
	"path"
	"strings"
)

// hosts expose Atom feeds at the same /releases.atom and /tags.atom paths.
var hosts = map[string]bool{
	"github.com":   true,
	"gitea.com":    true,
	"codeberg.org": true,
}

// FeedURL rewrites a repo, releases, or tags URL to its Atom feed;
// a bare repo URL defaults to its releases feed.
func FeedURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !hosts[strings.ToLower(u.Host)] {
		return raw
	}
	if strings.HasSuffix(u.Path, ".atom") || strings.HasSuffix(u.Path, ".rss") {
		return raw
	}

	segs := strings.Split(strings.Trim(u.Path, "/"), "/")
	switch {
	case len(segs) == 2: // owner/repo
		u.Path = "/" + path.Join(segs[0], segs[1], "releases.atom")
	case len(segs) == 3 && (segs[2] == "releases" || segs[2] == "tags"):
		u.Path += ".atom"
	default:
		return raw
	}
	return u.String()
}
