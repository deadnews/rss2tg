package main

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"

	"github.com/mmcdole/gofeed"

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

/sub &lt;url&gt; [link|pw|text] [shorts] [exclude:w1,w2] [include:w1,w2] — subscribe to feed
/unsub &lt;url&gt; — unsubscribe from feed
/list — list subscriptions
/format &lt;link|pw|text&gt; — change format for all subs
/help — show this message

YouTube channel URLs are auto-resolved to their Atom feed. YouTube Shorts are filtered by default; pass <code>shorts</code> to include them.

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

	switch cmd {
	case "/start", "/help":
		bot.reply(ctx, msg.Chat.ID, helpText)
	case "/sub":
		bot.handleSub(ctx, msg.Chat.ID, parts[1:])
	case "/unsub":
		bot.handleUnsub(ctx, msg.Chat.ID, parts[1:])
	case "/list":
		bot.handleList(ctx, msg.Chat.ID)
	case "/format":
		bot.handleFormat(ctx, msg.Chat.ID, parts[1:])
	}
}

const (
	subUsage    = "Usage: /sub &lt;url&gt; [link|pw|text] [shorts] [exclude:w1,w2] [include:w1,w2]"
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

func (bot *Bot) handleSub(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		bot.reply(ctx, chatID, subUsage)
		return
	}

	opts, ok := parseSubArgs(args[1:])
	if !ok {
		bot.reply(ctx, chatID, subUsage)
		return
	}

	url, err := youtube.ResolveURL(ctx, args[0])
	if err != nil {
		slog.Error("Failed to resolve YouTube URL", "url", args[0], "error", err)
		bot.reply(ctx, chatID, "Failed to subscribe.")
		return
	}

	feed, err := bot.parser.ParseURLWithContext(url, ctx)
	if err != nil {
		slog.Error("Failed to parse feed", "url", url, "error", err)
		bot.reply(ctx, chatID, "Failed to subscribe.")
		return
	}

	sub := store.Sub{
		URL:     url,
		Title:   feed.Title,
		Format:  opts.format,
		Shorts:  opts.shorts,
		Exclude: opts.exclude,
		Include: opts.include,
	}
	existed, err := bot.store.AddSub(chatID, &sub)
	if err != nil {
		slog.Error("Failed to add subscription", "error", err)
		bot.reply(ctx, chatID, "Failed to subscribe.")
		return
	}

	verb := "Subscribed to"
	if existed {
		verb = "Updated subscription for"
	}
	bot.reply(ctx, chatID, fmt.Sprintf("%s %s (%s)", verb, url, opts.format))
	bot.deliverInitialEntries(ctx, url, feed, []store.ChatFeed{{
		ChatID:  chatID,
		Format:  opts.format,
		Shorts:  opts.shorts,
		Exclude: opts.exclude,
		Include: opts.include,
	}})
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

func (bot *Bot) handleUnsub(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 {
		bot.reply(ctx, chatID, unsubUsage)
		return
	}

	url, err := youtube.ResolveURL(ctx, args[0])
	if err != nil {
		// Resolution failed. Try the raw URL as-is.
		url = args[0]
	}

	existed, err := bot.store.RemoveSub(chatID, url)
	if err != nil {
		slog.Error("Failed to remove subscription", "error", err)
		bot.reply(ctx, chatID, "Failed to unsubscribe.")
		return
	}

	if !existed {
		bot.reply(ctx, chatID, "Not subscribed to "+url)
		return
	}

	bot.reply(ctx, chatID, "Unsubscribed from "+url)
}

func (bot *Bot) handleList(ctx context.Context, chatID int64) {
	subs, err := bot.store.ListSubs(chatID)
	if err != nil {
		slog.Error("Failed to list subscriptions", "error", err)
		bot.reply(ctx, chatID, "Failed to list subscriptions.")
		return
	}

	if len(subs) == 0 {
		bot.reply(ctx, chatID, "No subscriptions.")
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

	bot.reply(ctx, chatID, b.String())
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

func (bot *Bot) handleFormat(ctx context.Context, chatID int64, args []string) {
	if len(args) == 0 || !validFormats[args[0]] {
		bot.reply(ctx, chatID, formatUsage)
		return
	}

	count, err := bot.store.SetFormat(chatID, args[0])
	if err != nil {
		slog.Error("Failed to set format", "error", err)
		bot.reply(ctx, chatID, "Failed to set format.")
		return
	}

	bot.reply(ctx, chatID, fmt.Sprintf("Updated %d subscription(s) to %s", count, args[0]))
}

func (bot *Bot) reply(ctx context.Context, chatID int64, text string) {
	if err := bot.tg.SendMessage(ctx, chatID, text, true); err != nil {
		slog.Error("Failed to send message", "error", err, "chat_id", chatID)
	}
}
