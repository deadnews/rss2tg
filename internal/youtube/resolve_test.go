package youtube

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveURL(t *testing.T) {
	t.Run("non-YouTube URL passes through", func(t *testing.T) {
		got, err := ResolveURL(t.Context(), "https://example.com/feed.xml")
		require.NoError(t, err)
		assert.Equal(t, "https://example.com/feed.xml", got)
	})

	t.Run("spoofed YouTube path in another host passes through", func(t *testing.T) {
		in := "https://evil.com/?u=youtube.com/channel/UCSrZ3UV4jOidv8ppoVuvW9Q"
		got, err := ResolveURL(t.Context(), in)
		require.NoError(t, err)
		assert.Equal(t, in, got)
	})

	t.Run("feeds URL passes through", func(t *testing.T) {
		in := "https://www.youtube.com/feeds/videos.xml?channel_id=UCxxx"
		got, err := ResolveURL(t.Context(), in)
		require.NoError(t, err)
		assert.Equal(t, in, got)
	})

	t.Run("channel URL resolves directly without HTTP", func(t *testing.T) {
		got, err := ResolveURL(t.Context(),
			"https://www.youtube.com/channel/UCSrZ3UV4jOidv8ppoVuvW9Q")
		require.NoError(t, err)
		assert.Equal(t, feedURLBase+"UCSrZ3UV4jOidv8ppoVuvW9Q", got)
	})

	t.Run("m.youtube.com channel URL resolves", func(t *testing.T) {
		got, err := ResolveURL(t.Context(),
			"https://m.youtube.com/channel/UCSrZ3UV4jOidv8ppoVuvW9Q")
		require.NoError(t, err)
		assert.Equal(t, feedURLBase+"UCSrZ3UV4jOidv8ppoVuvW9Q", got)
	})
}

func TestFetchChannelPage(t *testing.T) {
	t.Run("returns body on 200", func(t *testing.T) {
		const body = `<meta itemprop="channelId" content="UCabc123">`
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Mozilla/5.0 (rss2tg)", r.Header.Get("User-Agent"))
			_, _ = w.Write([]byte(body))
		}))
		defer ts.Close()

		got, err := fetchChannelPage(t.Context(), ts.URL)
		require.NoError(t, err)
		assert.Equal(t, body, string(got))
	})

	t.Run("errors on non-200", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "nope", http.StatusNotFound)
		}))
		defer ts.Close()

		_, err := fetchChannelPage(t.Context(), ts.URL)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "404")
	})

	t.Run("errors when server unreachable", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		ts.Close()

		_, err := fetchChannelPage(t.Context(), ts.URL)
		require.Error(t, err)
	})
}

func TestResolveURLHandleNotFound(t *testing.T) {
	// /@handle/ path on a non-YouTube host falls through the YouTube-host
	// gate before reaching the network — covers the early-return branch.
	got, err := ResolveURL(t.Context(), "https://example.com/@somehandle")
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/@somehandle", got)
}

func TestResolveURLChannelTrailingPath(t *testing.T) {
	got, err := ResolveURL(t.Context(),
		"https://www.youtube.com/channel/UCSrZ3UV4jOidv8ppoVuvW9Q/videos")
	require.NoError(t, err)
	assert.Equal(t, feedURLBase+"UCSrZ3UV4jOidv8ppoVuvW9Q", got)
}

func TestResolveURLChannelNonUC(t *testing.T) {
	// /channel/ with non-UC id should fall through to handle check and return raw.
	in := "https://www.youtube.com/channel/notUCprefixed"
	got, err := ResolveURL(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestResolveURLNonHandleNonChannel(t *testing.T) {
	in := "https://www.youtube.com/watch?v=abc"
	got, err := ResolveURL(t.Context(), in)
	require.NoError(t, err)
	assert.Equal(t, in, got)
}

func TestParseChannelID(t *testing.T) {
	t.Run("channelId meta", func(t *testing.T) {
		body := []byte(`<meta itemprop="channelId" content="UCSrZ3UV4jOidv8ppoVuvW9Q">`)
		id, ok := parseChannelID(body)
		require.True(t, ok)
		assert.Equal(t, "UCSrZ3UV4jOidv8ppoVuvW9Q", id)
	})

	t.Run("identifier meta", func(t *testing.T) {
		body := []byte(`<meta itemprop="identifier" content="UCabc123">`)
		id, ok := parseChannelID(body)
		require.True(t, ok)
		assert.Equal(t, "UCabc123", id)
	})

	t.Run("missing", func(t *testing.T) {
		_, ok := parseChannelID([]byte(`<html><head></head></html>`))
		assert.False(t, ok)
	})
}
