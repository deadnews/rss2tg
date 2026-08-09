package main

import (
	"cmp"
	"context"
	"fmt"
	"html"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/mmcdole/gofeed"

	"github.com/deadnews/rss2tg/internal/format"
	"github.com/deadnews/rss2tg/internal/github"
	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
	"github.com/deadnews/rss2tg/internal/youtube"
)

const initialSendLimit = 3

const (
	formatLink    = "link"
	formatPreview = "pw"
	formatText    = "text"
	formatQuote   = "quote"
)

var validFormats = map[string]bool{
	formatLink:    true,
	formatPreview: true,
	formatText:    true,
	formatQuote:   true,
}

const helpText = `<b>Available commands:</b>

<code>/sub &lt;url&gt; [link|pw|text|quote] [shorts] [nolive] [latest] [exclude:w1,w2] [include:w1,w2]</code> — subscribe to feed
<code>/unsub &lt;url&gt;</code> — unsubscribe from feed
<code>/list</code> — list subscriptions
<code>/help</code> — show this message

<b>Feeds</b>
• YouTube channel URLs auto-resolve to their Atom feed.
• Shorts are filtered by default — add <code>shorts</code> to include them.
• Live streams are included by default — add <code>nolive</code> to filter them out.
• GitHub repo URLs auto-resolve to their releases Atom feed.
• Add <code>latest</code> to send only releases labeled latest.

<b>Filters</b>
• Match anywhere in the title, case-insensitively; exclude wins over include.

<b>Topics</b>
• <code>/sub</code> from General creates a topic per feed; inside a topic subscribes there.
• <code>/list</code> from General shows every topic.`

// scope is the chat topic a command was issued from.
type scope struct {
	chatID   int64
	threadID int
	general  bool // a forum's General topic, from which commands span every topic
	private  bool
}

func newScope(msg *telegram.Message) scope {
	s := scope{chatID: msg.Chat.ID, private: msg.Chat.Type == "private"}
	// Scope the command to its forum topic; General topic reports no thread.
	if msg.IsTopicMessage {
		s.threadID = msg.MessageThreadID
	}
	s.general = msg.Chat.IsForum && s.threadID == 0
	return s
}

// handleCommand dispatches a bot command from an authorized sender.
func (bot *Bot) handleCommand(ctx context.Context, msg *telegram.Message) {
	parts := strings.Fields(msg.Text)
	if len(parts) == 0 {
		return
	}

	cmd := parts[0]
	// Strip @botname suffix (e.g. "/help@mybot").
	if i := strings.IndexByte(cmd, '@'); i > 0 {
		cmd = cmd[:i]
	}
	if !strings.HasPrefix(cmd, "/") {
		return
	}
	if !bot.authorized(ctx, msg) {
		return
	}

	s := newScope(msg)
	switch cmd {
	case "/start", "/help":
		bot.reply(ctx, s, helpText)
	case "/sub":
		bot.handleSub(ctx, s, parts[1:])
	case "/unsub":
		bot.handleUnsub(ctx, s, parts[1:])
	case "/list":
		bot.handleList(ctx, s)
	}
}

// authorized reports whether msg may run commands: the manager in a private or
// group chat, or a channel post from a channel the manager administers.
func (bot *Bot) authorized(ctx context.Context, msg *telegram.Message) bool {
	if msg.From != nil {
		return msg.From.ID == bot.cfg.Manager
	}
	admin, err := bot.tg.IsChatAdmin(ctx, msg.Chat.ID, bot.cfg.Manager)
	if err != nil {
		slog.Warn("Failed to verify channel admin", "chat_id", msg.Chat.ID, "error", err)
		return false
	}
	return admin
}

const (
	subUsage   = "Usage: /sub &lt;url&gt; [link|pw|text|quote] [shorts] [nolive] [latest] [exclude:w1,w2] [include:w1,w2]"
	unsubUsage = "Usage: /unsub &lt;url&gt;"
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
	latest  bool
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
		case arg == "latest":
			out.latest = true
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

func (bot *Bot) handleSub(ctx context.Context, s scope, args []string) {
	if len(args) == 0 {
		bot.reply(ctx, s, subUsage)
		return
	}

	opts, ok := parseSubArgs(args[1:])
	if !ok {
		bot.reply(ctx, s, subUsage)
		return
	}

	url, err := resolveFeedURL(ctx, args[0])
	if err != nil {
		slog.Error("Failed to resolve feed URL", "url", args[0], "error", err)
		bot.reply(ctx, s, "Failed to subscribe.")
		return
	}

	feed, err := bot.parseFeed(ctx, url, opts.latest)
	if err != nil {
		slog.Error("Failed to parse feed", "url", url, "error", err)
		bot.reply(ctx, s, "Failed to subscribe.")
		return
	}

	// In a forum's General topic, give each feed its own topic.
	if s.general {
		threadID, ok := bot.forumTopicFor(ctx, s, url, feed.Title)
		if !ok {
			return
		}
		s.threadID, s.general = threadID, false
	}

	sub := store.Sub{
		URL:     url,
		Title:   feed.Title,
		Format:  opts.format,
		Shorts:  opts.shorts,
		NoLive:  opts.noLive,
		Latest:  opts.latest,
		Exclude: opts.exclude,
		Include: opts.include,
	}
	existed, err := bot.store.AddSub(s.chatID, s.threadID, &sub)
	if err != nil {
		slog.Error("Failed to add subscription", "error", err)
		bot.reply(ctx, s, "Failed to subscribe.")
		return
	}

	// An update only changes options; new entries wait for the next poll cycle.
	if existed {
		bot.reply(ctx, s, fmt.Sprintf("Updated subscription for %s (%s)", html.EscapeString(url), sub.Format))
		return
	}
	bot.reply(ctx, s, fmt.Sprintf("Subscribed to %s (%s)", html.EscapeString(url), sub.Format))

	// Deliver to every chat subscribed to the URL.
	newChat := sub.ChatFeed(s.chatID, s.threadID)
	chats := []store.ChatFeed{newChat}
	if feeds, err := bot.store.AllFeeds(); err == nil {
		chats = feeds[url]
	} else {
		slog.Error("Failed to get feeds", "error", err)
	}
	bot.deliverInitialEntries(ctx, url, feed, &newChat, chats)
}

// forumTopicFor returns the feed's existing topic or creates one named after it.
func (bot *Bot) forumTopicFor(ctx context.Context, s scope, feedURL, title string) (threadID int, ok bool) {
	existing, found, err := bot.store.FindFeedThread(s.chatID, feedURL)
	if err != nil {
		slog.Error("Failed to find feed thread", "chat_id", s.chatID, "error", err)
		bot.reply(ctx, s, "Failed to subscribe.")
		return 0, false
	}
	if found {
		return existing, true
	}

	name := cmp.Or(title, feedURL)
	threadID, err = bot.tg.CreateForumTopic(ctx, s.chatID, name)
	if err != nil {
		slog.Error("Failed to create forum topic", "chat_id", s.chatID, "error", err)
		bot.reply(ctx, s, "Failed to create topic. The bot must be an admin with Manage Topics.")
		return 0, false
	}
	// Telegram auto-pins the creation message; it is cleared reactively on the service message.
	bot.reply(ctx, s, fmt.Sprintf("Created topic <b>%s</b>", html.EscapeString(name)))
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

// deliverInitialEntries delivers the newest initialSendLimit entries the new
// subscriber's filters accept and marks the rest seen, to avoid flooding.
func (bot *Bot) deliverInitialEntries(ctx context.Context, feedURL string, feed *gofeed.Feed, newChat *store.ChatFeed, chats []store.ChatFeed) {
	remaining := initialSendLimit
	for _, item := range feed.Items {
		if remaining > 0 && accepts(item, newChat) {
			remaining--
			continue
		}
		if err := bot.store.MarkSeen(feedURL, itemGUID(item)); err != nil {
			slog.Error("Failed to mark seen", "url", feedURL, "error", err)
		}
	}
	bot.deliverNew(ctx, feedURL, feed, chats)
}

func (bot *Bot) handleUnsub(ctx context.Context, s scope, args []string) {
	if len(args) == 0 {
		bot.reply(ctx, s, unsubUsage)
		return
	}

	url, err := resolveFeedURL(ctx, args[0])
	if err != nil {
		slog.Error("Failed to resolve feed URL", "url", args[0], "error", err)
		bot.reply(ctx, s, "Failed to unsubscribe.")
		return
	}

	// From General, remove from the feed's own topic but reply in General.
	target := s.threadID
	if s.general {
		id, found, err := bot.store.FindFeedThread(s.chatID, url)
		if err != nil {
			slog.Error("Failed to find feed thread", "chat_id", s.chatID, "error", err)
			bot.reply(ctx, s, "Failed to unsubscribe.")
			return
		}
		if found {
			target = id
		}
	}

	existed, err := bot.store.RemoveSub(s.chatID, target, url)
	if err != nil {
		slog.Error("Failed to remove subscription", "error", err)
		bot.reply(ctx, s, "Failed to unsubscribe.")
		return
	}

	if !existed {
		bot.reply(ctx, s, "Not subscribed to "+html.EscapeString(url))
		return
	}

	bot.reply(ctx, s, "Unsubscribed from "+html.EscapeString(url))
}

func (bot *Bot) handleList(ctx context.Context, s scope) {
	if s.private {
		bot.listAllSubs(ctx, s)
		return
	}

	var subs []store.Sub
	var err error
	// From a forum's General topic, list every topic's subs.
	if s.general {
		subs, err = bot.store.ChatSubs(s.chatID)
	} else {
		subs, err = bot.store.ListSubs(s.chatID, s.threadID)
	}
	if err != nil {
		slog.Error("Failed to list subscriptions", "error", err)
		bot.reply(ctx, s, "Failed to list subscriptions.")
		return
	}

	if len(subs) == 0 {
		bot.reply(ctx, s, "No subscriptions.")
		return
	}

	var b strings.Builder
	for i := range subs {
		if i > 0 {
			b.WriteString("\n")
		}
		writeSub(&b, &subs[i])
	}

	bot.reply(ctx, s, b.String())
}

// listAllSubs replies with every subscription across all chats, grouped by chat.
func (bot *Bot) listAllSubs(ctx context.Context, s scope) {
	subs, err := bot.store.AllSubs()
	if err != nil {
		slog.Error("Failed to list subscriptions", "error", err)
		bot.reply(ctx, s, "Failed to list subscriptions.")
		return
	}
	if len(subs) == 0 {
		bot.reply(ctx, s, "No subscriptions.")
		return
	}

	slices.SortFunc(subs, func(a, b store.ChatFeed) int {
		return cmp.Or(cmp.Compare(a.ChatID, b.ChatID), cmp.Compare(a.Title, b.Title), cmp.Compare(a.URL, b.URL))
	})

	labels := make(map[int64]string)
	var b strings.Builder
	for i := range subs {
		cf := &subs[i]
		if i == 0 || cf.ChatID != subs[i-1].ChatID {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString("● <b>")
			b.WriteString(html.EscapeString(bot.chatLabel(ctx, labels, cf.ChatID)))
			b.WriteString("</b>\n")
		}
		b.WriteString("\n")
		writeSub(&b, &cf.Sub)
	}

	bot.reply(ctx, s, b.String())
}

// chatLabel resolves a chat's title, or its numeric ID when unavailable.
func (bot *Bot) chatLabel(ctx context.Context, cache map[int64]string, chatID int64) string {
	if label, ok := cache[chatID]; ok {
		return label
	}
	label := strconv.FormatInt(chatID, 10)
	if chat, err := bot.tg.GetChat(ctx, chatID); err != nil {
		slog.Warn("Failed to resolve chat title", "chat_id", chatID, "error", err)
	} else if chat.Title != "" {
		label = chat.Title
	}
	cache[chatID] = label
	return label
}

// resolveFeedURL derives the stored key /sub and /unsub must agree on.
func resolveFeedURL(ctx context.Context, raw string) (string, error) {
	url, err := youtube.ResolveURL(ctx, raw)
	if err != nil {
		return "", fmt.Errorf("resolve feed URL: %w", err)
	}
	return normalizeURL(github.FeedURL(url)), nil
}

// normalizeURL trims trailing slashes so a feed and its slash variant are one sub.
func normalizeURL(url string) string {
	return strings.TrimRight(url, "/")
}

// writeSub renders a sub as an optional bold title above its /sub line.
func writeSub(b *strings.Builder, sub *store.Sub) {
	if sub.Title != "" {
		b.WriteString("<b>")
		b.WriteString(html.EscapeString(sub.Title))
		b.WriteString("</b>\n")
	}
	b.WriteString("<code>")
	b.WriteString(html.EscapeString(formatSubCommand(sub)))
	b.WriteString("</code>\n")
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
	if sub.Latest {
		b.WriteString(" latest")
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

// splitMessages packs blank-line-separated blocks into messages within limit
// bytes, so a long reply splits rather than truncates; bytes bound the UTF-16 limit.
func splitMessages(text string, limit int) []string {
	var chunks []string
	var b strings.Builder
	for block := range strings.SplitSeq(text, "\n\n") {
		if b.Len() > 0 && b.Len()+2+len(block) > limit { // +2 for the "\n\n" join
			chunks = append(chunks, b.String())
			b.Reset()
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(block)
	}
	if b.Len() > 0 {
		chunks = append(chunks, b.String())
	}
	return chunks
}

// reply sends text to a chat, splitting long replies across several messages.
func (bot *Bot) reply(ctx context.Context, s scope, text string) {
	for _, chunk := range splitMessages(text, format.MessageLimit) {
		chunk = format.TruncateHTML(chunk, format.MessageLimit)
		if err := bot.tg.SendMessage(ctx, s.chatID, s.threadID, chunk, true); err != nil {
			slog.Error("Failed to send message", "error", err, "chat_id", s.chatID)
		}
	}
}
