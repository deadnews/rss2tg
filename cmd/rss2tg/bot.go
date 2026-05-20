package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/deadnews/rss2tg/internal/store"
	"github.com/deadnews/rss2tg/internal/telegram"
)

const (
	cleanSeenEvery = 24 * time.Hour
	pollBackoff    = 5 * time.Second
)

// Bot orchestrates feed checks and Telegram updates.
type Bot struct {
	cfg    *Config
	tg     *telegram.Client
	store  *store.Store
	parser *gofeed.Parser
}

// NewBot creates a new Bot instance.
func NewBot(cfg *Config, tg *telegram.Client, st *store.Store) *Bot {
	return &Bot{
		cfg:    cfg,
		tg:     tg,
		store:  st,
		parser: gofeed.NewParser(),
	}
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
			timer := time.NewTimer(pollBackoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}

		for _, u := range updates {
			msg := u.Message
			if msg == nil {
				msg = u.ChannelPost
			}
			if msg != nil {
				bot.handleCommand(ctx, msg)
			}
			offset = u.UpdateID + 1
		}
	}
}

func (bot *Bot) checkFeedsLoop(ctx context.Context) {
	bot.checkFeeds(ctx)

	feedTicker := time.NewTicker(bot.cfg.Interval)
	defer feedTicker.Stop()
	cleanTicker := time.NewTicker(cleanSeenEvery)
	defer cleanTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping feed check loop")
			return
		case <-feedTicker.C:
			bot.checkFeeds(ctx)
		case <-cleanTicker.C:
			if err := bot.store.CleanSeen(); err != nil {
				slog.Error("Failed to clean seen entries", "error", err)
			}
		}
	}
}
