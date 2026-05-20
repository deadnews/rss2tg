package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
)

// sentMessage captures a message sent via the Telegram API mock.
type sentMessage struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

const commandTestFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Command Test Feed</title>
    <link>https://example.com</link>
  </channel>
</rss>`

func testBot(t *testing.T) (*Bot, *[]sentMessage, string) {
	t.Helper()

	var mu sync.Mutex
	var sent []sentMessage

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/feed.xml":
			w.Header().Set("Content-Type", "application/xml")
			_, _ = w.Write([]byte(commandTestFeed))
		case r.Method == http.MethodPost:
			var msg struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&msg)
			mu.Lock()
			sent = append(sent, sentMessage{ChatID: msg.ChatID, Text: msg.Text})
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
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

	return bot, &sent, ts.URL
}

func TestHandleCommandUnauthorized(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 999},
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	assert.Empty(t, *sent)
}

func TestHandleCommandChannelPost(t *testing.T) {
	bot, sent, _ := testBot(t)

	// Channel posts have no From — should be allowed (only admins can post).
	bot.handleCommand(t.Context(), &telegram.Message{
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Available commands")
}

func TestHandleHelp(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Available commands")
}

func TestHandleStart(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/start",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Available commands")
}

func TestHandleHelpWithBotSuffix(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/help@mybot",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Available commands")
}

func TestHandleSub(t *testing.T) {
	bot, sent, baseURL := testBot(t)
	feedURL := baseURL + "/feed.xml"

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Subscribed")

	subs, err := bot.store.ListSubs(100)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, feedURL, subs[0].URL)
	assert.Equal(t, "link", subs[0].Format)
	assert.Equal(t, "Command Test Feed", subs[0].Title)
}

func TestHandleListShowsTitle(t *testing.T) {
	bot, sent, baseURL := testBot(t)
	feedURL := baseURL + "/feed.xml"

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	require.GreaterOrEqual(t, len(*sent), 2)
	listMsg := (*sent)[len(*sent)-1].Text
	assert.Contains(t, listMsg, "<b>Command Test Feed</b>")
	assert.Contains(t, listMsg, feedURL)
	assert.Contains(t, listMsg, "[link]")
}

func TestHandleSubWithFormat(t *testing.T) {
	bot, sent, baseURL := testBot(t)
	feedURL := baseURL + "/feed.xml"

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL + " pw",
	})

	require.Len(t, *sent, 1)

	subs, err := bot.store.ListSubs(100)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, feedURL, subs[0].URL)
	assert.Equal(t, "pw", subs[0].Format)
}

func TestHandleSubNoArgs(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Usage")
}

func TestHandleSubInvalidFormat(t *testing.T) {
	bot, sent, baseURL := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + baseURL + "/feed.xml nope",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Usage")

	subs, err := bot.store.ListSubs(100)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestHandleSubInvalidFeed(t *testing.T) {
	bot, sent, baseURL := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + baseURL + "/missing.xml",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Failed to subscribe")

	subs, err := bot.store.ListSubs(100)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestHandleUnsub(t *testing.T) {
	bot, sent, _ := testBot(t)

	err := bot.store.AddSub(100, store.Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub https://example.com/feed.xml",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Unsubscribed")
}

func TestHandleUnsubNotFound(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub https://example.com/nope.xml",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Not subscribed")
}

func TestHandleUnsubNoArgs(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Usage")
}

func TestHandleList(t *testing.T) {
	bot, sent, _ := testBot(t)

	err := bot.store.AddSub(100, store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	err = bot.store.AddSub(100, store.Sub{URL: "https://b.com/feed", Format: "pw"})
	require.NoError(t, err)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "https://a.com/feed")
	assert.Contains(t, (*sent)[0].Text, "https://b.com/feed")
}

func TestHandleListEmpty(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "No subscriptions")
}

func TestHandleFormat(t *testing.T) {
	bot, sent, _ := testBot(t)

	err := bot.store.AddSub(100, store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/format pw",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Updated 1")

	subs, err := bot.store.ListSubs(100)
	require.NoError(t, err)
	assert.Equal(t, "pw", subs[0].Format)
}

func TestHandleFormatNoArgs(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/format",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Usage")
}

func TestHandleFormatInvalidArg(t *testing.T) {
	bot, sent, _ := testBot(t)

	bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/format invalid",
	})

	require.Len(t, *sent, 1)
	assert.Contains(t, (*sent)[0].Text, "Usage")
}
