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
	assert.NotNil(t, bot.feedClient)
	assert.Equal(t, cfg, bot.cfg)
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
