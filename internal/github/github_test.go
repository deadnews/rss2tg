package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFeedURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "bare repo defaults to releases",
			in:   "https://github.com/deadnews/rss2tg",
			want: "https://github.com/deadnews/rss2tg/releases.atom",
		},
		{
			name: "trailing slash",
			in:   "https://github.com/deadnews/rss2tg/",
			want: "https://github.com/deadnews/rss2tg/releases.atom",
		},
		{
			name: "releases gets .atom",
			in:   "https://github.com/deadnews/rss2tg/releases",
			want: "https://github.com/deadnews/rss2tg/releases.atom",
		},
		{
			name: "tags gets .atom",
			in:   "https://github.com/deadnews/rss2tg/tags",
			want: "https://github.com/deadnews/rss2tg/tags.atom",
		},
		{
			name: "host case-insensitive",
			in:   "https://GitHub.com/deadnews/rss2tg/releases",
			want: "https://GitHub.com/deadnews/rss2tg/releases.atom",
		},
		{
			name: "already a feed passes through",
			in:   "https://github.com/deadnews/rss2tg/releases.atom",
			want: "https://github.com/deadnews/rss2tg/releases.atom",
		},
		{
			name: "non-github host passes through",
			in:   "https://gitea.com/gitea/runner/releases",
			want: "https://gitea.com/gitea/runner/releases",
		},
		{
			name: "commits not rewritten",
			in:   "https://github.com/deadnews/rss2tg/commits",
			want: "https://github.com/deadnews/rss2tg/commits",
		},
		{
			name: "owner only passes through",
			in:   "https://github.com/deadnews",
			want: "https://github.com/deadnews",
		},
		{
			name: "specific release page passes through",
			in:   "https://github.com/deadnews/rss2tg/releases/tag/v0.0.4",
			want: "https://github.com/deadnews/rss2tg/releases/tag/v0.0.4",
		},
		{
			name: "non-URL passes through",
			in:   "not a url",
			want: "not a url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FeedURL(tt.in))
		})
	}
}
