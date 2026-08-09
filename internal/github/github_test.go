package github

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestLatestURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "releases feed",
			in:   "https://github.com/deadnews/rss2tg/releases.atom",
			want: "https://github.com/deadnews/rss2tg/releases/latest",
			ok:   true,
		},
		{
			name: "host case-insensitive",
			in:   "https://GitHub.com/deadnews/rss2tg/releases.atom",
			want: "https://GitHub.com/deadnews/rss2tg/releases/latest",
			ok:   true,
		},
		{
			name: "tags feed has no latest",
			in:   "https://github.com/deadnews/rss2tg/tags.atom",
		},
		{
			name: "specific release page is not a feed",
			in:   "https://github.com/deadnews/rss2tg/releases/tag/v0.0.4",
		},
		{
			name: "non-github host",
			in:   "https://gitea.com/gitea/runner/releases.atom",
		},
		{
			name: "non-URL",
			in:   "not a url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LatestURL(tt.in)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveLatest(t *testing.T) {
	const tagURL = "https://github.com/deadnews/rss2tg/releases/tag/v0.0.4"

	tests := []struct {
		name   string
		status int
		loc    string
		want   string
		errMsg string
	}{
		{name: "redirect to tag", status: http.StatusFound, loc: tagURL, want: tagURL},
		{name: "no releases", status: http.StatusNotFound},
		{name: "only prereleases", status: http.StatusFound, loc: "https://github.com/deadnews/rss2tg/releases"},
		{name: "server error", status: http.StatusInternalServerError, errMsg: "status 500"},
		{name: "unexpected ok", status: http.StatusOK, errMsg: "status 200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tt.loc != "" {
					w.Header().Set("Location", tt.loc)
				}
				w.WriteHeader(tt.status)
			}))
			defer ts.Close()

			got, err := ResolveLatest(t.Context(), ts.URL)
			if tt.errMsg != "" {
				require.ErrorContains(t, err, tt.errMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveLatestUnreachable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	ts.Close()

	_, err := ResolveLatest(t.Context(), ts.URL)
	require.ErrorContains(t, err, "fetch latest release")
}
