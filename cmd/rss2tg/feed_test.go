package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
)

const testRSS = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Test Feed</title>
    <link>https://example.com</link>
    <item>
      <title>Post One</title>
      <link>https://example.com/1</link>
      <guid>guid-1</guid>
      <description>First post.</description>
    </item>
    <item>
      <title>Post Two</title>
      <link>https://example.com/2</link>
      <guid>guid-2</guid>
      <description>Second post.</description>
    </item>
  </channel>
</rss>`

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "test", name))
	require.NoError(t, err)
	return data
}

type testFeedBot struct {
	bot   *Bot
	store *store.Store
	mu    *sync.Mutex
	sent  *[]sentMessage
	ts    *httptest.Server
	mux   *http.ServeMux
}

func newTestFeedBot(t *testing.T) *testFeedBot {
	t.Helper()

	var mu sync.Mutex
	var sent []sentMessage

	hnXML := readTestdata(t, "hn.xml")
	redditAtom := readTestdata(t, "reddit.atom")
	youtubeAtom := readTestdata(t, "youtube.atom")

	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(testRSS))
	})
	mux.HandleFunc("/hn.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write(hnXML)
	})
	mux.HandleFunc("/reddit.atom", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write(redditAtom)
	})
	mux.HandleFunc("/youtube.atom", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write(youtubeAtom)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var raw struct {
				ChatID  int64  `json:"chat_id"`
				Text    string `json:"text"`
				Caption string `json:"caption"`
			}
			_ = json.NewDecoder(r.Body).Decode(&raw)
			text := raw.Text
			if text == "" {
				text = raw.Caption
			}
			mu.Lock()
			sent = append(sent, sentMessage{ChatID: raw.ChatID, Text: text})
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	st, err := store.New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = st.Close() })

	tg := telegram.NewClient("test-token")
	tg.BaseURL = ts.URL

	bot := &Bot{
		cfg:    &Config{Manager: 42},
		tg:     tg,
		store:  st,
		parser: gofeed.NewParser(),
	}

	return &testFeedBot{bot: bot, store: st, mu: &mu, sent: &sent, ts: ts, mux: mux}
}

func (tb *testFeedBot) getSent() []sentMessage {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return append([]sentMessage(nil), *tb.sent...)
}

func (tb *testFeedBot) resetSent() {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	*tb.sent = nil
}

func TestCheckFeeds(t *testing.T) {
	tb := newTestFeedBot(t)

	err := tb.store.AddSub(100, store.Sub{URL: tb.ts.URL + "/feed.xml", Format: "link"})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())
	assert.Len(t, tb.getSent(), 2)

	// Run again — nothing new.
	tb.resetSent()
	tb.bot.checkFeeds(t.Context())
	assert.Empty(t, tb.getSent())
}

func TestCheckFeedsPreviewFormat(t *testing.T) {
	tb := newTestFeedBot(t)

	err := tb.store.AddSub(100, store.Sub{URL: tb.ts.URL + "/feed.xml", Format: "pw"})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())

	sent := tb.getSent()
	require.Len(t, sent, 2)
	assert.Contains(t, sent[0].Text, `<a href=`)
	assert.Contains(t, sent[0].Text, "<b>")
	assert.Contains(t, sent[0].Text, "Test Feed")
}

func TestSubscribeFeedSendsOnlyLatest(t *testing.T) {
	const rss = `<?xml version="1.0"?>
<rss version="2.0"><channel><title>T</title>
<item><title>P1</title><link>https://e.com/1</link><guid>1</guid></item>
<item><title>P2</title><link>https://e.com/2</link><guid>2</guid></item>
<item><title>P3</title><link>https://e.com/3</link><guid>3</guid></item>
<item><title>P4</title><link>https://e.com/4</link><guid>4</guid></item>
<item><title>P5</title><link>https://e.com/5</link><guid>5</guid></item>
</channel></rss>`

	tb := newTestFeedBot(t)
	tb.mux.HandleFunc("/seed.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(rss))
	})

	feedURL := tb.ts.URL + "/seed.xml"
	feed, err := tb.bot.parser.ParseURLWithContext(feedURL, t.Context())
	require.NoError(t, err)
	tb.bot.deliverInitialEntries(t.Context(), feedURL, feed, []store.ChatFeed{{ChatID: 100, Format: "link"}})

	assert.Len(t, tb.getSent(), initialSendLimit)

	for _, guid := range []string{"1", "2", "3", "4", "5"} {
		seen, err := tb.store.IsSeen(feedURL, guid)
		require.NoError(t, err)
		assert.True(t, seen, "guid %s should be seen after subscribe", guid)
	}
}

func TestItemGUID(t *testing.T) {
	t.Run("uses GUID", func(t *testing.T) {
		item := &gofeed.Item{GUID: "abc", Link: "https://example.com"}
		assert.Equal(t, "abc", itemGUID(item))
	})

	t.Run("falls back to link", func(t *testing.T) {
		item := &gofeed.Item{Link: "https://example.com"}
		assert.Equal(t, "https://example.com", itemGUID(item))
	})

	t.Run("falls back to title-published", func(t *testing.T) {
		item := &gofeed.Item{Title: "Post", Published: "2024-01-01"}
		assert.Equal(t, "Post-2024-01-01", itemGUID(item))
	})
}

func TestCheckFeedsTextFormat(t *testing.T) {
	tb := newTestFeedBot(t)

	err := tb.store.AddSub(100, store.Sub{URL: tb.ts.URL + "/hn.xml", Format: "text"})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())

	sent := tb.getSent()
	require.Len(t, sent, 1)
	text := sent[0].Text
	// Title header with link.
	assert.Contains(t, text, `<b>Hacker News Daily Top 30 @2026-04-11</b>`)
	// Numbered items with preserved links from real HN data.
	assert.Contains(t, text, "1. ")
	assert.Contains(t, text, `<a href="https://www.numerique.gouv.fr/`)
	assert.Contains(t, text, "France Launches Government Linux Desktop Plan")
	assert.Contains(t, text, "2. ")
	assert.Contains(t, text, "1D Chess")
	assert.Contains(t, text, "3. ")
	assert.Contains(t, text, "Artemis II safely splashes down")
	assert.Contains(t, text, "\n")
}

func TestCheckFeedsYouTubeFiltersShorts(t *testing.T) {
	tb := newTestFeedBot(t)

	feedURL := tb.ts.URL + "/youtube.atom"
	err := tb.store.AddSub(100, store.Sub{URL: feedURL, Format: "link"})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())

	sent := tb.getSent()
	// 5 entries total, 2 of which are /shorts/ → 3 delivered.
	require.Len(t, sent, 3)
	for _, msg := range sent {
		assert.NotContains(t, msg.Text, "/shorts/", "shorts entry should not be delivered")
	}
	assert.Contains(t, sent[len(sent)-1].Text, "Turkish Airlines plane evacuated")
}

func TestCheckFeedsYouTubeIncludesShortsWhenEnabled(t *testing.T) {
	tb := newTestFeedBot(t)

	feedURL := tb.ts.URL + "/youtube.atom"
	err := tb.store.AddSub(100, store.Sub{URL: feedURL, Format: "link", Shorts: true})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())

	sent := tb.getSent()
	require.Len(t, sent, 5)

	var shorts int
	for _, msg := range sent {
		if strings.Contains(msg.Text, "/shorts/") {
			shorts++
		}
	}
	assert.Equal(t, 2, shorts)
}

func TestCheckFeedsYouTubeMarksShortsSeen(t *testing.T) {
	// Even when all chats filter shorts, the GUID must be marked seen,
	// otherwise we'd re-process the same items every poll cycle.
	tb := newTestFeedBot(t)

	feedURL := tb.ts.URL + "/youtube.atom"
	err := tb.store.AddSub(100, store.Sub{URL: feedURL, Format: "link"})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())
	tb.resetSent()

	// Second run: no entries should be sent (all marked seen, including filtered shorts).
	tb.bot.checkFeeds(t.Context())
	assert.Empty(t, tb.getSent())

	for _, guid := range []string{"yt:video:vsdApGt9d4k", "yt:video:8Gdm2FXCzo4"} {
		seen, err := tb.store.IsSeen(feedURL, guid)
		require.NoError(t, err)
		assert.True(t, seen, "shorts guid %s should be marked seen", guid)
	}
}

func TestCheckFeedsPWReddit(t *testing.T) {
	tb := newTestFeedBot(t)

	err := tb.store.AddSub(100, store.Sub{URL: tb.ts.URL + "/reddit.atom", Format: "pw"})
	require.NoError(t, err)

	tb.bot.checkFeeds(t.Context())

	sent := tb.getSent()
	require.Len(t, sent, 2)
	// First item (oldest): "Map of light pollution" — image-only post.
	assert.Contains(t, sent[0].Text, "<b>Map of light pollution around the world</b>")
	assert.Contains(t, sent[0].Text, `<a href="https://www.reddit.com/gallery/1kulwar">[link]</a>`)
	assert.Contains(t, sent[0].Text, "[comments]</a>")
	assert.Contains(t, sent[0].Text, "\nvia <b>top scoring links : MapPorn</b>")
	// Second item: "Google Maps" — has submitted by with link.
	assert.Contains(t, sent[1].Text, "Google Maps")
	assert.Contains(t, sent[1].Text, `<a href="https://www.reddit.com/user/Mackelowsky">`)
	assert.Contains(t, sent[1].Text, "/u/Mackelowsky")
	assert.Contains(t, sent[1].Text, `<a href="https://i.redd.it/6j31j4o2qrlf1.jpeg">[link]</a>`)
}
