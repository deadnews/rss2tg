package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
)

func TestNewBot(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer st.Close()

	cfg := &Config{BotToken: "token", Manager: 42, Interval: 5 * time.Minute}
	tg := telegram.NewClient("token")
	bot := NewBot(cfg, tg, st)

	assert.NotNil(t, bot)
	assert.NotNil(t, bot.parser)
	assert.Equal(t, cfg, bot.cfg)
}

const githubReleasesAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <title>Release notes from rss2tg</title>
  <entry>
    <id>tag:github.com,2008:Repository/1/v2.0.0-rc1</id>
    <updated>2026-08-06T00:00:00Z</updated>
    <link rel="alternate" type="text/html" href="https://github.com/deadnews/rss2tg/releases/tag/v2.0.0-rc1"/>
    <title>v2.0.0-rc1</title>
  </entry>
  <entry>
    <id>tag:github.com,2008:Repository/1/v1.2.0</id>
    <updated>2026-08-05T00:00:00Z</updated>
    <link rel="alternate" type="text/html" href="https://github.com/deadnews/rss2tg/releases/tag/v1.2.0"/>
    <title>v1.2.0</title>
  </entry>
</feed>`

func TestKeepLatestRelease(t *testing.T) {
	tests := []struct {
		name      string
		latest    func(w http.ResponseWriter)
		wantTitle string
		wantErr   bool
	}{
		{
			name: "keeps only the latest release",
			latest: func(w http.ResponseWriter) {
				w.Header().Set("Location", "https://github.com/deadnews/rss2tg/releases/tag/v1.2.0")
				w.WriteHeader(http.StatusFound)
			},
			wantTitle: "v1.2.0",
		},
		{
			name:   "no release marked latest",
			latest: func(w http.ResponseWriter) { w.WriteHeader(http.StatusNotFound) },
		},
		{
			name: "latest outside the feed window",
			latest: func(w http.ResponseWriter) {
				w.Header().Set("Location", "https://github.com/deadnews/rss2tg/releases/tag/v0.1.0")
				w.WriteHeader(http.StatusFound)
			},
		},
		{
			name:    "resolve failure",
			latest:  func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { tt.latest(w) }))
			defer ts.Close()

			feed, err := gofeed.NewParser().ParseString(githubReleasesAtom)
			require.NoError(t, err)

			err = keepLatestRelease(t.Context(), feed, ts.URL)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantTitle == "" {
				assert.Empty(t, feed.Items)
				return
			}
			require.Len(t, feed.Items, 1)
			assert.Equal(t, tt.wantTitle, feed.Items[0].Title)
		})
	}
}

func TestKeepLatestReleaseDeliversOnLaterPromotion(t *testing.T) {
	var link atomic.Value
	link.Store("")

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if loc, _ := link.Load().(string); loc != "" {
			w.Header().Set("Location", loc)
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	reduce := func(t *testing.T) *gofeed.Feed {
		t.Helper()
		feed, err := gofeed.NewParser().ParseString(githubReleasesAtom)
		require.NoError(t, err)
		require.NoError(t, keepLatestRelease(t.Context(), feed, ts.URL))
		return feed
	}

	require.Empty(t, reduce(t).Items)

	link.Store("https://github.com/deadnews/rss2tg/releases/tag/v2.0.0-rc1")
	feed := reduce(t)
	require.Len(t, feed.Items, 1)
	assert.Equal(t, "v2.0.0-rc1", feed.Items[0].Title)
}

func TestRunValidationFailure(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "description": "Unauthorized"})
	}))
	defer ts.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer st.Close()

	tg := telegram.NewClient("bad-token")
	tg.BaseURL = ts.URL

	bot := NewBot(&Config{Interval: time.Second}, tg, st)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	err = bot.Run(ctx)
	require.Error(t, err)
	assert.ErrorContains(t, err, "Unauthorized")
}

func TestPollUpdatesContextCancellation(t *testing.T) {
	var reqCount atomic.Int32
	var notifyOnce sync.Once
	firstRequest := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
		notifyOnce.Do(func() { close(firstRequest) })
	}))
	defer ts.Close()

	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	defer st.Close()

	tg := telegram.NewClient("token")
	tg.BaseURL = ts.URL

	bot := NewBot(&Config{Interval: time.Hour}, tg, st)

	ctx, cancel := context.WithCancel(t.Context())

	done := make(chan struct{})
	go func() {
		bot.pollUpdates(ctx)
		close(done)
	}()

	select {
	case <-firstRequest:
	case <-time.After(2 * time.Second):
		t.Fatal("pollUpdates did not issue initial request")
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollUpdates did not stop after context cancellation")
	}

	assert.Positive(t, reqCount.Load())
}
