package main

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"github.com/mmcdole/gofeed"

	"github.com/deadnews/rss2tg/internal/format"
	"github.com/deadnews/rss2tg/internal/githost"
	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
	"github.com/deadnews/rss2tg/internal/youtube"
)

const initialSendLimit = 3

const (
	formatLink    = "link"
	formatPreview = "pw"
	formatText    = "text"
)

var validFormats = map[string]bool{
	formatLink:    true,
	formatPreview: true,
	formatText:    true,
}

const helpText = `<b>Available commands:</b>

<code>/sub &lt;url&gt; [link|pw|text] [shorts] [nolive] [exclude:w1,w2] [include:w1,w2]</code> — subscribe to feed
<code>/unsub &lt;url&gt;</code> — unsubscribe from feed
<code>/list</code> — list subscriptions
<code>/format &lt;link|pw|text&gt;</code> — change format for all subs
<code>/help</code> — show this message

YouTube channel URLs are auto-resolved to their Atom feed. YouTube Shorts are filtered by default; pass <code>shorts</code> to include them. Live streams are included by default; pass <code>nolive</code> to filter them out.

GitHub/Gitea/Codeberg repo URLs are auto-resolved to their releases Atom feed; pass a <code>/releases</code> or <code>/tags</code> URL to choose.

In a forum group, <code>/sub</code> from the General topic creates a topic per feed; run it inside a topic to subscribe there.

Filters match whole words in the title (case-insensitive). Exclude wins over include.`

// handleCommand dispatches a bot command from an authorized user.
func (bot *Bot) handleCommand(ctx context.Context, msg *telegram.Message) {
	// Private/group chats: verify sender is the manager.
	// Channel posts (From == nil): trusted, only admins can post.
	if msg.From != nil && msg.From.ID != bot.cfg.Manager {
		return
	}

	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	// Strip @botname suffix (e.g. "/help@mybot").
	if i := strings.IndexByte(cmd, '@'); i > 0 {
		cmd = cmd[:i]
	}

	// Scope the command to its forum topic; General topic reports no thread.
	var threadID int
	if msg.IsTopicMessage {
		threadID = msg.MessageThreadID
	}

	switch cmd {
	case "/start", "/help":
		bot.reply(ctx, msg.Chat.ID, threadID, helpText)
	case "/sub":
		bot.handleSub(ctx, msg.Chat.ID, threadID, msg.Chat.IsForum, parts[1:])
	case "/unsub":
		bot.handleUnsub(ctx, msg.Chat.ID, threadID, parts[1:])
	case "/list":
		bot.handleList(ctx, msg.Chat.ID, threadID, msg.Chat.IsForum)
	case "/format":
		bot.handleFormat(ctx, msg.Chat.ID, threadID, parts[1:])
	}
}

const (
	subUsage    = "Usage: /sub &lt;url&gt; [link|pw|text] [shorts] [nolive] [exclude:w1,w2] [include:w1,w2]"
	unsubUsage  = "Usage: /unsub &lt;url&gt;"
	formatUsage = "Usage: /format &lt;link|pw|text&gt;"
)

const (
	prefixExclude = "exclude:"
	prefixInclude = "include:"
)

// parsedSubArgs holds optional /sub flags after the URL.
type parsedSubArgs struct {
	format  string
	shorts  bool
	noLive  bool
	exclude []string
	include []string
}

// parseSubArgs parses optional /sub flags in any order.
func parseSubArgs(args []string) (parsedSubArgs, bool) {
	out := parsedSubArgs{format: formatLink}
	for _, arg := range args {
		switch {
		case arg == "shorts":
			out.shorts = true
		case arg == "nolive":
			out.noLive = true
		case validFormats[arg]:
			out.format = arg
		case strings.HasPrefix(arg, prefixExclude):
			words, ok := parseFilterArg(arg[len(prefixExclude):])
			if !ok {
				return parsedSubArgs{}, false
			}
			out.exclude = words
		case strings.HasPrefix(arg, prefixInclude):
			words, ok := parseFilterArg(arg[len(prefixInclude):])
			if !ok {
				return parsedSubArgs{}, false
			}
			out.include = words
		default:
			return parsedSubArgs{}, false
		}
	}
	return out, true
}

func (bot *Bot) handleSub(ctx context.Context, chatID int64, threadID int, isForum bool, args []string) {
	if len(args) == 0 {
		bot.reply(ctx, chatID, threadID, subUsage)
		return
	}

	opts, ok := parseSubArgs(args[1:])
	if !ok {
		bot.reply(ctx, chatID, threadID, subUsage)
		return
	}

	url, err := youtube.ResolveURL(ctx, githost.FeedURL(args[0]))
	if err != nil {
		slog.Error("Failed to resolve feed URL", "url", args[0], "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to subscribe.")
		return
	}

	feed, err := bot.parseFeed(ctx, url)
	if err != nil {
		slog.Error("Failed to parse feed", "url", url, "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to subscribe.")
		return
	}

	// In a forum's General topic, give each feed its own topic.
	if isForum && threadID == 0 {
		var ok bool
		if threadID, ok = bot.forumTopicFor(ctx, chatID, url, feed.Title); !ok {
			return
		}
	}

	sub := store.Sub{
		URL:     url,
		Title:   feed.Title,
		Format:  opts.format,
		Shorts:  opts.shorts,
		NoLive:  opts.noLive,
		Exclude: opts.exclude,
		Include: opts.include,
	}
	existed, err := bot.store.AddSub(chatID, threadID, &sub)
	if err != nil {
		slog.Error("Failed to add subscription", "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to subscribe.")
		return
	}

	verb := "Subscribed to"
	if existed {
		verb = "Updated subscription for"
	}
	bot.reply(ctx, chatID, threadID, fmt.Sprintf("%s %s (%s)", verb, url, sub.Format))
	bot.deliverInitialEntries(ctx, url, feed, []store.ChatFeed{sub.ChatFeed(chatID, threadID)})
}

// forumTopicFor returns the feed's existing topic or creates one named after it.
func (bot *Bot) forumTopicFor(ctx context.Context, chatID int64, feedURL, title string) (threadID int, ok bool) {
	existing, found, err := bot.store.FindFeedThread(chatID, feedURL)
	if err != nil {
		slog.Error("Failed to find feed thread", "chat_id", chatID, "error", err)
		bot.reply(ctx, chatID, 0, "Failed to subscribe.")
		return 0, false
	}
	if found {
		return existing, true
	}

	name := cmp.Or(title, feedURL)
	threadID, err = bot.tg.CreateForumTopic(ctx, chatID, name)
	if err != nil {
		slog.Error("Failed to create forum topic", "chat_id", chatID, "error", err)
		bot.reply(ctx, chatID, 0, "Failed to create topic. The bot must be an admin with Manage Topics.")
		return 0, false
	}
	// Telegram auto-pins the creation message; it is cleared reactively on the service message.
	bot.reply(ctx, chatID, 0, fmt.Sprintf("Created topic <b>%s</b>", html.EscapeString(name)))
	return threadID, true
}

// clearTopicCreationPin removes the topic-creation service message
// that Telegram auto-pins whenever a forum topic is created.
func (bot *Bot) clearTopicCreationPin(ctx context.Context, msg *telegram.Message) {
	if err := bot.tg.UnpinAllForumTopicMessages(ctx, msg.Chat.ID, msg.MessageThreadID); err != nil {
		slog.Warn("Failed to unpin topic creation message",
			"chat_id", msg.Chat.ID, "thread_id", msg.MessageThreadID, "error", err)
	}
}

// deliverInitialEntries delivers the latest initialSendLimit entries
// and marks the rest seen, to avoid flooding a new subscriber.
func (bot *Bot) deliverInitialEntries(ctx context.Context, feedURL string, feed *gofeed.Feed, chats []store.ChatFeed) {
	for _, item := range feed.Items[min(len(feed.Items), initialSendLimit):] {
		if err := bot.store.MarkSeen(feedURL, itemGUID(item)); err != nil {
			slog.Error("Failed to mark seen", "url", feedURL, "error", err)
		}
	}
	bot.deliverNew(ctx, feedURL, feed, chats)
}

func (bot *Bot) handleUnsub(ctx context.Context, chatID int64, threadID int, args []string) {
	if len(args) == 0 {
		bot.reply(ctx, chatID, threadID, unsubUsage)
		return
	}

	url, err := youtube.ResolveURL(ctx, githost.FeedURL(args[0]))
	if err != nil {
		slog.Error("Failed to resolve feed URL", "url", args[0], "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to unsubscribe.")
		return
	}

	existed, err := bot.store.RemoveSub(chatID, threadID, url)
	if err != nil {
		slog.Error("Failed to remove subscription", "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to unsubscribe.")
		return
	}

	if !existed {
		bot.reply(ctx, chatID, threadID, "Not subscribed to "+url)
		return
	}

	bot.reply(ctx, chatID, threadID, "Unsubscribed from "+url)
}

func (bot *Bot) handleList(ctx context.Context, chatID int64, threadID int, isForum bool) {
	var subs []store.Sub
	var err error
	// From a forum's General topic, list every topic's subs.
	if isForum && threadID == 0 {
		subs, err = bot.store.ChatSubs(chatID)
	} else {
		subs, err = bot.store.ListSubs(chatID, threadID)
	}
	if err != nil {
		slog.Error("Failed to list subscriptions", "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to list subscriptions.")
		return
	}

	if len(subs) == 0 {
		bot.reply(ctx, chatID, threadID, "No subscriptions.")
		return
	}

	var b strings.Builder
	for i := range subs {
		if i > 0 {
			b.WriteString("\n")
		}
		sub := &subs[i]
		if sub.Title != "" {
			b.WriteString("<b>")
			b.WriteString(html.EscapeString(sub.Title))
			b.WriteString("</b>\n")
		}
		b.WriteString("<code>")
		b.WriteString(html.EscapeString(formatSubCommand(sub)))
		b.WriteString("</code>\n")
	}

	bot.reply(ctx, chatID, threadID, b.String())
}

// formatSubCommand renders a sub as the /sub line that recreates it.
func formatSubCommand(sub *store.Sub) string {
	var b strings.Builder
	b.WriteString("/sub ")
	b.WriteString(sub.URL)
	b.WriteString(" ")
	b.WriteString(sub.Format)
	if sub.Shorts {
		b.WriteString(" shorts")
	}
	if sub.NoLive {
		b.WriteString(" nolive")
	}
	if len(sub.Exclude) > 0 {
		b.WriteString(" exclude:")
		b.WriteString(strings.Join(sub.Exclude, ","))
	}
	if len(sub.Include) > 0 {
		b.WriteString(" include:")
		b.WriteString(strings.Join(sub.Include, ","))
	}
	return b.String()
}

func (bot *Bot) handleFormat(ctx context.Context, chatID int64, threadID int, args []string) {
	if len(args) == 0 || !validFormats[args[0]] {
		bot.reply(ctx, chatID, threadID, formatUsage)
		return
	}

	count, err := bot.store.SetFormat(chatID, threadID, args[0])
	if err != nil {
		slog.Error("Failed to set format", "error", err)
		bot.reply(ctx, chatID, threadID, "Failed to set format.")
		return
	}

	bot.reply(ctx, chatID, threadID, fmt.Sprintf("Updated %d subscription(s) to %s", count, args[0]))
}

func (bot *Bot) reply(ctx context.Context, chatID int64, threadID int, text string) {
	if err := bot.tg.SendMessage(ctx, chatID, threadID, format.TruncateHTML(text, format.MessageLimit), true); err != nil {
		slog.Error("Failed to send message", "error", err, "chat_id", chatID)
	}
}
