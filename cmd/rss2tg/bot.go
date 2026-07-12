package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
)

const (
	pollRetryDelay   = 5 * time.Second
	feedFetchTimeout = 40 * time.Second
	feedMaxBytes     = 10 << 20 // 10 MiB
)

// Bot orchestrates feed checks and Telegram updates.
type Bot struct {
	cfg    *Config
	tg     *telegram.Client
	store  *store.Store
	parser *gofeed.Parser // shared across goroutines; configure only in NewBot
}

// NewBot creates a new Bot instance.
func NewBot(cfg *Config, tg *telegram.Client, st *store.Store) *Bot {
	parser := gofeed.NewParser()
	parser.Client = &http.Client{Timeout: feedFetchTimeout}
	parser.MaxByteSize = feedMaxBytes
	return &Bot{
		cfg:    cfg,
		tg:     tg,
		store:  st,
		parser: parser,
	}
}

// parseFeed fetches and parses a feed.
func (bot *Bot) parseFeed(ctx context.Context, url string) (*gofeed.Feed, error) {
	feed, err := bot.parser.ParseURLWithContext(url, ctx)
	if err != nil {
		return nil, fmt.Errorf("parse feed: %w", err)
	}
	return feed, nil
}

// Run validates the bot token and runs the feed-check and update-polling loops.
func (bot *Bot) Run(ctx context.Context) error {
	me, err := bot.tg.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("validate bot token: %w", err)
	}
	slog.Info("Bot started", "username", me.Username, "id", me.ID)

	var wg sync.WaitGroup
	wg.Go(func() { bot.checkFeedsLoop(ctx) })
	wg.Go(func() { bot.pollUpdates(ctx) })
	wg.Wait()
	return nil
}

func (bot *Bot) pollUpdates(ctx context.Context) {
	defer slog.Info("Stopping update polling")

	var offset int64
	for {
		updates, err := bot.tg.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("Failed to get updates", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(pollRetryDelay):
			}
			continue
		}

		for _, u := range updates {
			msg := u.Message
			if msg == nil {
				msg = u.ChannelPost
			}
			if msg != nil {
				bot.routeMessage(ctx, msg)
			}
			offset = u.UpdateID + 1
		}
	}
}

// routeMessage clears the auto-pin on forum topic-creation messages;
// everything else is handled as a command.
func (bot *Bot) routeMessage(ctx context.Context, msg *telegram.Message) {
	if msg.ForumTopicCreated != nil {
		bot.clearTopicCreationPin(ctx, msg)
		return
	}
	bot.handleCommand(ctx, msg)
}

func (bot *Bot) checkFeedsLoop(ctx context.Context) {
	bot.checkFeeds(ctx)

	feedTicker := time.NewTicker(bot.cfg.Interval)
	defer feedTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping feed check loop")
			return
		case <-feedTicker.C:
			bot.checkFeeds(ctx)
		}
	}
}
