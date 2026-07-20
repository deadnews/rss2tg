package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mmcdole/gofeed"

	"github.com/deadnews/rss2tg/internal/format"
	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
	"github.com/deadnews/rss2tg/internal/youtube"
)

// checkFeeds fetches all subscribed feeds and sends new entries.
func (bot *Bot) checkFeeds(ctx context.Context) {
	feeds, err := bot.store.AllFeeds()
	if err != nil {
		slog.Error("Failed to get feeds", "error", err)
		return
	}

	for url, chats := range feeds {
		if ctx.Err() != nil {
			return
		}
		feed, err := bot.parseFeed(ctx, url)
		if err != nil {
			slog.Error("Failed to parse feed", "url", url, "error", err)
			continue
		}
		bot.deliverNew(ctx, url, feed, chats)
		// Cap the seen set at twice the feed length so every served entry stays remembered.
		if err := bot.store.TrimSeen(url, max(2*len(feed.Items), 100)); err != nil {
			slog.Error("Failed to trim seen", "url", url, "error", err)
		}
	}
}

// entry is one feed item prepared for delivery.
type entry struct {
	url  string // subscription URL, not the item link
	guid string
	item *gofeed.Item
	feed *gofeed.Feed
	meta string // video meta line, when enriched
	live bool
}

// deliverNew sends unseen entries oldest-first; each is marked seen only after at least one chat receives it.
func (bot *Bot) deliverNew(ctx context.Context, feedURL string, feed *gofeed.Feed, chats []store.ChatFeed) {
	// Feeds list newest-first; reverse so messages appear chronologically in chat.
	for _, item := range slices.Backward(feed.Items) {
		guid := itemGUID(item)

		seen, err := bot.store.IsSeen(feedURL, guid)
		if err != nil {
			slog.Error("Failed to check seen", "url", feedURL, "guid", guid, "error", err)
			continue
		}
		if seen {
			continue
		}

		e := entry{url: feedURL, guid: guid, item: item, feed: feed}
		recipients := recipientsFor(item, chats)
		if len(recipients) > 0 {
			if info := bot.videoInfo(ctx, item.Link); info != nil {
				e.meta = info.MetaLine()
				e.live = info.Stream
			}
		}

		// Retry next cycle when a transient failure left the entry undelivered.
		if !bot.deliverEntry(ctx, &e, recipients) {
			continue
		}
		if err := bot.store.MarkSeen(feedURL, guid); err != nil {
			slog.Error("Failed to mark seen", "url", feedURL, "guid", guid, "error", err)
		}
	}
}

// deliverEntry sends e to each accepting chat; reports whether the entry may be
// marked seen: delivered at least once, or not held back by a transient failure.
func (bot *Bot) deliverEntry(ctx context.Context, e *entry, recipients []*store.ChatFeed) bool {
	var delivered, retryable bool
	for _, chat := range recipients {
		if e.live && chat.NoLive {
			continue
		}
		err := bot.sendEntry(ctx, e, chat)
		var apiErr *telegram.APIError
		switch {
		case err == nil:
			delivered = true
		case errors.As(err, &apiErr):
			slog.Warn("Dropping entry rejected by Telegram",
				"url", e.url, "guid", e.guid, "chat_id", chat.ChatID, "error", err)
		default:
			slog.Error("Failed to send entry",
				"url", e.url, "guid", e.guid, "chat_id", chat.ChatID, "error", err)
			retryable = true
		}
	}
	return delivered || !retryable
}

// recipientsFor returns the chats whose shorts and title filters accept the item.
func recipientsFor(item *gofeed.Item, chats []store.ChatFeed) []*store.ChatFeed {
	recipients := make([]*store.ChatFeed, 0, len(chats))
	for i := range chats {
		if accepts(item, &chats[i]) {
			recipients = append(recipients, &chats[i])
		}
	}
	return recipients
}

// accepts reports whether the chat's shorts and title filters accept the item.
func accepts(item *gofeed.Item, chat *store.ChatFeed) bool {
	if youtube.IsShort(item.Link) && !chat.Shorts {
		return false
	}
	return allow(item.Title, chat.Include, chat.Exclude)
}

func (bot *Bot) sendEntry(ctx context.Context, e *entry, chat *store.ChatFeed) error {
	var text string
	disablePreview := true
	switch chat.Format {
	case formatPreview:
		text = format.Preview(e.item, e.feed.Title, e.feed.Link)
		disablePreview = false
		if bot.sendPreviewPhoto(ctx, e.item, text, chat) {
			return nil
		}
	case formatText:
		text = format.Text(e.item)
	case formatQuote:
		text = format.Quote(e.item)
	default:
		text = format.Link(e.item, e.meta)
		disablePreview = false
	}
	text = format.TruncateHTML(text, format.MessageLimit)
	if err := bot.tg.SendMessage(ctx, chat.ChatID, chat.ThreadID, text, disablePreview); err != nil {
		return fmt.Errorf("send entry: %w", err)
	}
	return nil
}

// sendPreviewPhoto sends the preview as a photo with caption; false means
// no image or a failed send, and the caller falls back to a text message.
func (bot *Bot) sendPreviewPhoto(ctx context.Context, item *gofeed.Item, caption string, chat *store.ChatFeed) bool {
	img := format.ExtractImage(item)
	if img == "" {
		return false
	}
	caption = format.TruncateHTML(caption, format.CaptionLimit)
	if err := bot.tg.SendPhoto(ctx, chat.ChatID, chat.ThreadID, img, caption); err != nil {
		slog.Warn("Failed to send photo; falling back to message", "url", img, "error", err)
		return false
	}
	return true
}

// videoInfo fetches YouTube metadata for a link, or nil on missing key,
// non-YouTube link, or API error (the entry then sends unenriched).
func (bot *Bot) videoInfo(ctx context.Context, link string) *youtube.VideoInfo {
	if bot.cfg.YouTubeKey == "" {
		return nil
	}
	id, ok := youtube.ExtractVideoID(link)
	if !ok {
		return nil
	}
	info, err := youtube.FetchVideoInfo(ctx, bot.cfg.YouTubeKey, id)
	if err != nil {
		slog.Warn("Failed to fetch YouTube info", "id", id, "error", err)
		return nil
	}
	return info
}

func itemGUID(item *gofeed.Item) string {
	if item.GUID != "" {
		return item.GUID
	}
	if item.Link != "" {
		return item.Link
	}
	return item.Title + "-" + item.Published
}
