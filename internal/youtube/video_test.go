package youtube

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsShort(t *testing.T) {
	tests := map[string]bool{
		"https://www.youtube.com/shorts/abc123":    true,
		"https://www.youtube.com/watch?v=abc123":   false,
		"https://example.com/shorts/abc":           false,
		"https://www.youtube.com/feeds/videos.xml": false,
		"": false,
	}
	for url, want := range tests {
		assert.Equal(t, want, IsShort(url), url)
	}
}

func TestExtractVideoID(t *testing.T) {
	tests := map[string]struct {
		id string
		ok bool
	}{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ":         {"dQw4w9WgXcQ", true},
		"https://www.youtube.com/watch?v=abc&t=10s":           {"abc", true},
		"https://www.youtube.com/shorts/vsdApGt9d4k":          {"vsdApGt9d4k", true},
		"https://www.youtube.com/shorts/vsdApGt9d4k/":         {"vsdApGt9d4k", true},
		"https://m.youtube.com/watch?v=abc":                   {"abc", true},
		"https://youtu.be/dQw4w9WgXcQ":                        {"dQw4w9WgXcQ", true},
		"https://www.youtube.com/":                            {"", false},
		"https://example.com/watch?v=abc":                     {"", false},
		"https://www.youtube.com/feeds/videos.xml?channel_id": {"", false},
	}
	for link, want := range tests {
		id, ok := ExtractVideoID(link)
		assert.Equal(t, want.ok, ok, link)
		assert.Equal(t, want.id, id, link)
	}
}

func TestParseISODuration(t *testing.T) {
	tests := map[string]time.Duration{
		"PT5M33S":  5*time.Minute + 33*time.Second,
		"PT21M3S":  21*time.Minute + 3*time.Second,
		"PT1H2M3S": time.Hour + 2*time.Minute + 3*time.Second,
		"PT30S":    30 * time.Second,
		"PT2H":     2 * time.Hour,
		"P0D":      0,
		"":         0,
		"garbage":  0,
	}
	for in, want := range tests {
		assert.Equal(t, want, parseISODuration(in), in)
	}
}

func TestFormatDuration(t *testing.T) {
	tests := map[time.Duration]string{
		5*time.Minute + 33*time.Second:               "5:33",
		33 * time.Second:                             "0:33",
		time.Hour + 2*time.Minute + 3*time.Second:    "1:02:03",
		10*time.Hour + 5*time.Minute + 9*time.Second: "10:05:09",
	}
	for in, want := range tests {
		assert.Equal(t, want, formatDuration(in), want)
	}
}

func TestVideoInfoMetaLine(t *testing.T) {
	t.Run("regular video", func(t *testing.T) {
		v := &VideoInfo{LiveStatus: "none", Duration: 5*time.Minute + 33*time.Second}
		assert.Equal(t, "⏱️ <code>5:33</code>", v.MetaLine())
	})

	t.Run("regular video with zero duration is empty", func(t *testing.T) {
		v := &VideoInfo{LiveStatus: "none"}
		assert.Empty(t, v.MetaLine())
	})

	t.Run("upcoming with scheduled time", func(t *testing.T) {
		v := &VideoInfo{
			LiveStatus:     "upcoming",
			ScheduledStart: time.Date(2026, 5, 25, 18, 0, 0, 0, time.UTC),
		}
		assert.Equal(t, "📅 <code>2026-05-25 18:00 UTC</code>", v.MetaLine())
	})

	t.Run("upcoming without scheduled time is empty", func(t *testing.T) {
		v := &VideoInfo{LiveStatus: "upcoming"}
		assert.Empty(t, v.MetaLine())
	})

	t.Run("live", func(t *testing.T) {
		v := &VideoInfo{LiveStatus: "live"}
		assert.Equal(t, "🔴 LIVE", v.MetaLine())
	})

	t.Run("unknown status is empty", func(t *testing.T) {
		v := &VideoInfo{}
		assert.Empty(t, v.MetaLine())
	})
}

func TestFetchVideoInfo(t *testing.T) {
	const regularResp = `{"items":[{
		"snippet":{"liveBroadcastContent":"none"},
		"contentDetails":{"duration":"PT21M3S"}
	}]}`
	const liveResp = `{"items":[{
		"snippet":{"liveBroadcastContent":"live"},
		"contentDetails":{"duration":"P0D"},
		"liveStreamingDetails":{"scheduledStartTime":"2026-05-23T23:00:00Z","actualStartTime":"2026-05-23T22:50:11Z"}
	}]}`
	const upcomingResp = `{"items":[{
		"snippet":{"liveBroadcastContent":"upcoming"},
		"contentDetails":{"duration":"P0D"},
		"liveStreamingDetails":{"scheduledStartTime":"2026-05-25T18:00:00Z"}
	}]}`
	const emptyResp = `{"items":[]}`

	withServer := func(t *testing.T, body string, fn func()) {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "test-key", r.URL.Query().Get("key"))
			assert.NotEmpty(t, r.URL.Query().Get("id"))
			assert.Contains(t, r.URL.Query().Get("part"), "liveStreamingDetails")
			_, _ = w.Write([]byte(body))
		}))
		defer ts.Close()
		orig := apiURL
		apiURL = ts.URL
		defer func() { apiURL = orig }()
		fn()
	}

	t.Run("regular video", func(t *testing.T) {
		withServer(t, regularResp, func() {
			info, err := FetchVideoInfo(t.Context(), "test-key", "abc")
			require.NoError(t, err)
			assert.Equal(t, "none", info.LiveStatus)
			assert.Equal(t, 21*time.Minute+3*time.Second, info.Duration)
			assert.True(t, info.ScheduledStart.IsZero())
		})
	})

	t.Run("live stream", func(t *testing.T) {
		withServer(t, liveResp, func() {
			info, err := FetchVideoInfo(t.Context(), "test-key", "abc")
			require.NoError(t, err)
			assert.Equal(t, "live", info.LiveStatus)
		})
	})

	t.Run("upcoming stream", func(t *testing.T) {
		withServer(t, upcomingResp, func() {
			info, err := FetchVideoInfo(t.Context(), "test-key", "abc")
			require.NoError(t, err)
			assert.Equal(t, "upcoming", info.LiveStatus)
			assert.Equal(t,
				time.Date(2026, 5, 25, 18, 0, 0, 0, time.UTC),
				info.ScheduledStart.UTC())
		})
	})

	t.Run("video not found", func(t *testing.T) {
		withServer(t, emptyResp, func() {
			_, err := FetchVideoInfo(t.Context(), "test-key", "abc")
			require.Error(t, err)
		})
	})

	t.Run("non-200 errors", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "quota", http.StatusForbidden)
		}))
		defer ts.Close()
		orig := apiURL
		apiURL = ts.URL
		defer func() { apiURL = orig }()

		_, err := FetchVideoInfo(t.Context(), "test-key", "abc")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "403")
	})
}
