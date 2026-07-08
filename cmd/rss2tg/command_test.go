package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
)

const commandTestFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Command Test Feed</title>
    <link>https://example.com</link>
  </channel>
</rss>`

// newTestCmdBot returns an env serving an item-less feed at /cmd.xml,
// suitable for /sub command tests that only assert on the reply.
func newTestCmdBot(t *testing.T) *testBotEnv {
	t.Helper()
	env := newTestBotEnv(t)
	env.serveXML("/cmd.xml", []byte(commandTestFeed))
	return env
}

func TestHandleCommandUnauthorized(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 999},
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	assert.Empty(t, tb.getSent())
}

func TestHandleCommandChannelPost(t *testing.T) {
	tb := newTestCmdBot(t)
	tb.serveChatMember("administrator")

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Available commands")
}

func TestHandleCommandChannelPostNonManager(t *testing.T) {
	tb := newTestCmdBot(t)
	tb.serveChatMember("member")

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	assert.Empty(t, tb.getSent())
}

func TestHandleCommandChannelPostAdminCheckFails(t *testing.T) {
	tb := newTestCmdBot(t)
	tb.failChatMember()

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	assert.Empty(t, tb.getSent())
}

func TestHandleHelp(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/help",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Available commands")
}

func TestHandleStart(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/start",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Available commands")
}

func TestHandleHelpWithBotSuffix(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/help@mybot",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Available commands")
}

func TestHandleSub(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Subscribed")

	subs, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, feedURL, subs[0].URL)
	assert.Equal(t, "link", subs[0].Format)
	assert.Equal(t, "Command Test Feed", subs[0].Title)
}

func TestHandleSubDeliversInitialEntriesToExistingSubscribers(t *testing.T) {
	tb := newTestBotEnv(t)
	tb.serveXML("/feed.xml", []byte(testRSS))
	feedURL := tb.ts.URL + "/feed.xml"

	_, err := tb.store.AddSub(200, 0, &store.Sub{URL: feedURL, Format: "link"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	var toExisting []sentMessage
	for _, msg := range tb.getSent() {
		if msg.ChatID == 200 {
			toExisting = append(toExisting, msg)
		}
	}
	assert.Len(t, toExisting, 2)
}

func TestHandleSubEscapesURL(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml?a=1&b=2"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Subscribed to "+tb.ts.URL+"/cmd.xml?a=1&amp;b=2")
}

func TestHandleSubInTopicRoutesToThread(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From:            &telegram.User{ID: 42},
		Chat:            telegram.Chat{ID: 100},
		MessageThreadID: 5,
		IsTopicMessage:  true,
		Text:            "/sub " + feedURL,
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Equal(t, 5, sent[0].ThreadID, "reply should go to the originating topic")

	// Sub is scoped to the topic, not the General feed.
	topic, err := tb.store.ListSubs(100, 5)
	require.NoError(t, err)
	require.Len(t, topic, 1)
	assert.Equal(t, feedURL, topic[0].URL)

	general, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	assert.Empty(t, general)
}

func TestHandleSubInForumGeneralCreatesTopic(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100, IsForum: true},
		Text: "/sub " + feedURL,
	})

	// A topic named after the feed was created; General holds no sub.
	assert.Equal(t, []string{"Command Test Feed"}, tb.getTopics())
	general, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	assert.Empty(t, general)

	// The feed lives in the new topic.
	feeds, err := tb.store.AllFeeds()
	require.NoError(t, err)
	require.Len(t, feeds[feedURL], 1)
	thread := feeds[feedURL][0].ThreadID
	assert.NotZero(t, thread)

	// General gets a "Created topic" notice; the topic gets the subscription confirmation.
	sent := tb.getSent()
	require.Len(t, sent, 2)
	assert.Equal(t, 0, sent[0].ThreadID)
	assert.Contains(t, sent[0].Text, "Created topic")
	assert.Contains(t, sent[0].Text, "Command Test Feed")
	assert.Equal(t, thread, sent[1].ThreadID)
	assert.Contains(t, sent[1].Text, "Subscribed")
}

func TestHandleSubInForumGeneralReusesTopic(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	for range 2 {
		tb.bot.handleCommand(t.Context(), &telegram.Message{
			From: &telegram.User{ID: 42},
			Chat: telegram.Chat{ID: 100, IsForum: true},
			Text: "/sub " + feedURL,
		})
	}

	// Re-subscribing reuses the feed's topic instead of spawning a new one.
	assert.Len(t, tb.getTopics(), 1)
	feeds, err := tb.store.AllFeeds()
	require.NoError(t, err)
	assert.Len(t, feeds[feedURL], 1)
}

func TestHandleSubNonForumSkipsTopic(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	assert.Empty(t, tb.getTopics())
	general, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, general, 1)
}

func TestRouteMessageClearsTopicCreationPin(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.routeMessage(t.Context(), &telegram.Message{
		Chat:              telegram.Chat{ID: 100, IsForum: true},
		MessageThreadID:   7,
		IsTopicMessage:    true,
		ForumTopicCreated: &telegram.ForumTopicCreated{Name: "News"},
	})

	unpins := tb.getUnpins()
	require.Len(t, unpins, 1)
	assert.Equal(t, int64(100), unpins[0].ChatID)
	assert.Equal(t, 7, unpins[0].ThreadID)
	// A topic-creation service message is not treated as a command.
	assert.Empty(t, tb.getSent())
}

func TestHandleListShowsTitle(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL,
	})

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	sent := tb.getSent()
	require.GreaterOrEqual(t, len(sent), 2)
	listMsg := sent[len(sent)-1].Text
	assert.Contains(t, listMsg, "<b>Command Test Feed</b>")
	assert.Contains(t, listMsg, "<code>/sub "+feedURL+" link</code>")
}

func TestHandleSubWithFormat(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL + " pw",
	})

	require.Len(t, tb.getSent(), 1)

	subs, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, feedURL, subs[0].URL)
	assert.Equal(t, "pw", subs[0].Format)
}

func TestHandleSubNoArgs(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Usage")
}

func TestHandleSubInvalidFormat(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + tb.ts.URL + "/cmd.xml nope",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Usage")

	subs, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestHandleSubInvalidFeed(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + tb.ts.URL + "/missing.xml",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Failed to subscribe")

	subs, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestHandleUnsub(t *testing.T) {
	tb := newTestCmdBot(t)

	_, err := tb.store.AddSub(100, 0, &store.Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub https://example.com/feed.xml",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Unsubscribed")
}

func TestHandleUnsubFromForumGeneralRemovesTopicSub(t *testing.T) {
	tb := newTestCmdBot(t)

	_, err := tb.store.AddSub(100, 5, &store.Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100, IsForum: true},
		Text: "/unsub https://example.com/feed.xml",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Unsubscribed")
	assert.Equal(t, 0, sent[0].ThreadID, "reply should go to General where the command was issued")

	subs, err := tb.store.ListSubs(100, 5)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestHandleUnsubEscapesURL(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub https://example.com/feed?a=1&b=2",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Not subscribed to https://example.com/feed?a=1&amp;b=2")
}

func TestHandleUnsubNotFound(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub https://example.com/nope.xml",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Not subscribed")
}

func TestHandleUnsubNoArgs(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/unsub",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Usage")
}

func TestHandleList(t *testing.T) {
	tb := newTestCmdBot(t)

	_, err := tb.store.AddSub(100, 0, &store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = tb.store.AddSub(100, 0, &store.Sub{URL: "https://b.com/feed", Format: "pw"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "https://a.com/feed")
	assert.Contains(t, sent[0].Text, "https://b.com/feed")
}

func TestHandleListFromForumGeneralAggregates(t *testing.T) {
	tb := newTestCmdBot(t)

	_, err := tb.store.AddSub(100, 0, &store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = tb.store.AddSub(100, 5, &store.Sub{URL: "https://b.com/feed", Format: "pw"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100, IsForum: true},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "https://a.com/feed")
	assert.Contains(t, sent[0].Text, "https://b.com/feed")
}

func TestHandleListFromForumTopicStaysScoped(t *testing.T) {
	tb := newTestCmdBot(t)

	_, err := tb.store.AddSub(100, 5, &store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = tb.store.AddSub(100, 9, &store.Sub{URL: "https://b.com/feed", Format: "pw"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From:            &telegram.User{ID: 42},
		Chat:            telegram.Chat{ID: 100, IsForum: true},
		MessageThreadID: 5,
		IsTopicMessage:  true,
		Text:            "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "https://a.com/feed")
	assert.NotContains(t, sent[0].Text, "https://b.com/feed")
}

func TestHandleListInPrivateListsEveryChat(t *testing.T) {
	tb := newTestCmdBot(t)
	tb.serveChat(map[int64]string{-100: "News Group", -200: "Dev Group"})

	_, err := tb.store.AddSub(-100, 0, &store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = tb.store.AddSub(-100, 5, &store.Sub{URL: "https://b.com/feed", Format: "pw"})
	require.NoError(t, err)
	_, err = tb.store.AddSub(-200, 0, &store.Sub{URL: "https://c.com/feed", Format: "text"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 42, Type: "private"},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Equal(t, 0, sent[0].ThreadID)
	msg := sent[0].Text
	assert.Contains(t, msg, "<b>News Group</b>")
	assert.Contains(t, msg, "(topic 5)")
	assert.Contains(t, msg, "<b>Dev Group</b>")
	assert.Contains(t, msg, "https://a.com/feed")
	assert.Contains(t, msg, "https://b.com/feed")
	assert.Contains(t, msg, "https://c.com/feed")
}

func TestHandleListInPrivateShowsFeedTitles(t *testing.T) {
	tb := newTestCmdBot(t)
	tb.serveChat(map[int64]string{-100: "News Group"})

	_, err := tb.store.AddSub(-100, 0, &store.Sub{URL: "https://a.com/feed", Title: "Feed A", Format: "link"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 42, Type: "private"},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "<b>News Group</b>")
	assert.Contains(t, sent[0].Text, "<b>Feed A</b>")
	assert.Contains(t, sent[0].Text, "<code>/sub https://a.com/feed link</code>")
}

func TestHandleListInPrivateFallsBackToChatID(t *testing.T) {
	tb := newTestCmdBot(t)
	// Without a getChat handler, the lookup fails and the ID is used.
	_, err := tb.store.AddSub(-100, 0, &store.Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 42, Type: "private"},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "<b>-100</b>")
}

func TestHandleListInPrivateEmpty(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 42, Type: "private"},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "No subscriptions")
}

func TestHandleListEmpty(t *testing.T) {
	tb := newTestCmdBot(t)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "No subscriptions")
}

func TestParseSubArgs(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want parsedSubArgs
		ok   bool
	}{
		{
			name: "empty defaults to link",
			args: nil,
			want: parsedSubArgs{format: formatLink},
			ok:   true,
		},
		{
			name: "format only",
			args: []string{"pw"},
			want: parsedSubArgs{format: "pw"},
			ok:   true,
		},
		{
			name: "all options any order",
			args: []string{"include:go,rust", "shorts", "nolive", "exclude:Crypto,AI", "pw"},
			want: parsedSubArgs{
				format:  "pw",
				shorts:  true,
				noLive:  true,
				exclude: []string{"crypto", "ai"},
				include: []string{"go", "rust"},
			},
			ok: true,
		},
		{
			name: "invalid filter rejected",
			args: []string{"exclude:c++"},
			ok:   false,
		},
		{
			name: "unknown token rejected",
			args: []string{"nope"},
			ok:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseSubArgs(tc.args)
			assert.Equal(t, tc.ok, ok)
			if ok {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestHandleSubWithFilters(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/sub " + feedURL + " pw exclude:crypto,ai include:go",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text, "Subscribed")

	subs, err := tb.store.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, []string{"crypto", "ai"}, subs[0].Exclude)
	assert.Equal(t, []string{"go"}, subs[0].Include)
}

func TestHandleSubResubReplies(t *testing.T) {
	tb := newTestCmdBot(t)
	feedURL := tb.ts.URL + "/cmd.xml"

	for range 2 {
		tb.bot.handleCommand(t.Context(), &telegram.Message{
			From: &telegram.User{ID: 42},
			Chat: telegram.Chat{ID: 100},
			Text: "/sub " + feedURL,
		})
	}

	sent := tb.getSent()
	require.GreaterOrEqual(t, len(sent), 2)
	assert.Contains(t, sent[0].Text, "Subscribed to")
	assert.Contains(t, sent[1].Text, "Updated subscription for")
}

func TestHandleListRendersFilters(t *testing.T) {
	tb := newTestCmdBot(t)

	_, err := tb.store.AddSub(100, 0, &store.Sub{
		URL:     "https://a.com/feed",
		Title:   "Feed A",
		Format:  "pw",
		Shorts:  true,
		NoLive:  true,
		Exclude: []string{"crypto"},
		Include: []string{"go", "rust"},
	})
	require.NoError(t, err)

	tb.bot.handleCommand(t.Context(), &telegram.Message{
		From: &telegram.User{ID: 42},
		Chat: telegram.Chat{ID: 100},
		Text: "/list",
	})

	sent := tb.getSent()
	require.Len(t, sent, 1)
	assert.Contains(t, sent[0].Text,
		"<code>/sub https://a.com/feed pw shorts nolive exclude:crypto include:go,rust</code>")
}
