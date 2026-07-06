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

		recipients := recipientsFor(item, chats)

		var meta string
		var isLive bool
		if len(recipients) > 0 {
			if info := bot.videoInfo(ctx, item.Link); info != nil {
				meta = info.MetaLine()
				isLive = info.Stream
			}
		}

		var delivered, retryable bool
		for _, chat := range recipients {
			if isLive && chat.NoLive {
				continue
			}
			err := bot.sendEntry(ctx, item, feed.Title, feed.Link, meta, chat)
			var apiErr *telegram.APIError
			switch {
			case err == nil:
				delivered = true
			case errors.As(err, &apiErr):
				slog.Warn("Dropping entry rejected by Telegram",
					"url", feedURL, "guid", guid, "chat_id", chat.ChatID, "error", err)
			default:
				slog.Error("Failed to send entry",
					"url", feedURL, "guid", guid, "chat_id", chat.ChatID, "error", err)
				retryable = true
			}
		}

		// Retry next cycle only when a transient failure left the entry undelivered.
		if retryable && !delivered {
			continue
		}
		if err := bot.store.MarkSeen(feedURL, guid); err != nil {
			slog.Error("Failed to mark seen", "url", feedURL, "guid", guid, "error", err)
		}
	}
}

// recipientsFor returns the chats whose shorts and title filters accept the item.
func recipientsFor(item *gofeed.Item, chats []store.ChatFeed) []*store.ChatFeed {
	isShort := youtube.IsShort(item.Link)
	recipients := make([]*store.ChatFeed, 0, len(chats))
	for i := range chats {
		chat := &chats[i]
		if isShort && !chat.Shorts {
			continue
		}
		if !allow(item.Title, chat.Include, chat.Exclude) {
			continue
		}
		recipients = append(recipients, chat)
	}
	return recipients
}

func (bot *Bot) sendEntry(ctx context.Context, item *gofeed.Item, feedTitle, feedLink, meta string, chat *store.ChatFeed) error {
	var err error
	switch chat.Format {
	case formatPreview:
		caption := format.Preview(item, feedTitle, feedLink)
		if img := format.ExtractImage(item); img != "" {
			if err = bot.tg.SendPhoto(ctx, chat.ChatID, chat.ThreadID, img, format.TruncateHTML(caption, format.CaptionLimit)); err == nil {
				return nil
			}
			slog.Warn("SendPhoto failed, falling back to message", "url", img, "error", err)
		}
		err = bot.tg.SendMessage(ctx, chat.ChatID, chat.ThreadID, format.TruncateHTML(caption, format.MessageLimit), false)
	case formatText:
		err = bot.tg.SendMessage(ctx, chat.ChatID, chat.ThreadID, format.TruncateHTML(format.Text(item), format.MessageLimit), true)
	default:
		text := format.Link(item, meta)
		err = bot.tg.SendMessage(ctx, chat.ChatID, chat.ThreadID, format.TruncateHTML(text, format.MessageLimit), false)
	}
	if err != nil {
		return fmt.Errorf("send entry: %w", err)
	}
	return nil
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
		slog.Warn("YouTube enrichment failed", "id", id, "error", err)
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
