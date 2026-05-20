package main

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"github.com/mmcdole/gofeed"

	"github.com/deadnews/rss2tg/internal/format"
	"github.com/deadnews/rss2tg/internal/store"
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
		feed, err := bot.parser.ParseURLWithContext(url, ctx)
		if err != nil {
			slog.Error("Failed to parse feed", "url", url, "error", err)
			continue
		}
		bot.deliverNew(ctx, url, feed, chats)
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

		isShort := youtube.IsShort(item.Link)
		var attempted, delivered bool
		for _, chat := range chats {
			if isShort && !chat.Shorts {
				continue
			}
			attempted = true
			if err := bot.sendEntry(ctx, item, feed.Title, feed.Link, chat); err != nil {
				slog.Error("Failed to send entry",
					"url", feedURL, "guid", guid,
					"chat_id", chat.ChatID, "error", err,
				)
				continue
			}
			delivered = true
		}

		// Skip mark-seen only when every attempt failed (e.g. network), retry next cycle.
		if attempted && !delivered {
			continue
		}
		if err := bot.store.MarkSeen(feedURL, guid); err != nil {
			slog.Error("Failed to mark seen", "url", feedURL, "guid", guid, "error", err)
		}
	}
}

func (bot *Bot) sendEntry(ctx context.Context, item *gofeed.Item, feedTitle, feedLink string, chat store.ChatFeed) error {
	var err error
	switch chat.Format {
	case formatPreview:
		caption := format.Preview(item, feedTitle, feedLink)
		if img := format.ExtractImage(item); img != "" {
			if err = bot.tg.SendPhoto(ctx, chat.ChatID, img, caption); err == nil {
				return nil
			}
			slog.Warn("SendPhoto failed, falling back to message", "url", img, "error", err)
		}
		err = bot.tg.SendMessage(ctx, chat.ChatID, caption, false)
	case formatText:
		err = bot.tg.SendMessage(ctx, chat.ChatID, format.Text(item), true)
	default:
		err = bot.tg.SendMessage(ctx, chat.ChatID, format.Link(item), false)
	}
	if err != nil {
		return fmt.Errorf("send entry: %w", err)
	}
	return nil
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
