package main

import (
	"errors"
	"os"
	"strconv"
	"time"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	BotToken   string
	Manager    int64
	Interval   time.Duration
	DBPath     string
	YouTubeKey string
}

// LoadConfig loads configuration from environment variables.
func LoadConfig() (*Config, error) {
	token := os.Getenv("RSS2TG_TOKEN")
	if token == "" {
		return nil, errors.New("RSS2TG_TOKEN environment variable is required")
	}

	managerStr := os.Getenv("RSS2TG_MANAGER")
	if managerStr == "" {
		return nil, errors.New("RSS2TG_MANAGER environment variable is required")
	}

	manager, err := strconv.ParseInt(managerStr, 10, 64)
	if err != nil {
		return nil, errors.New("RSS2TG_MANAGER must be a valid integer")
	}

	interval := 10 * time.Minute
	if v := os.Getenv("RSS2TG_INTERVAL"); v != "" {
		interval, err = time.ParseDuration(v)
		if err != nil || interval <= 0 {
			return nil, errors.New("RSS2TG_INTERVAL must be a positive duration (e.g. 5m)")
		}
	}

	dbPath := os.Getenv("RSS2TG_DB_PATH")
	if dbPath == "" {
		dbPath = "rss2tg.db"
	}

	return &Config{
		BotToken:   token,
		Manager:    manager,
		Interval:   interval,
		DBPath:     dbPath,
		YouTubeKey: os.Getenv("RSS2TG_YOUTUBE_KEY"),
	}, nil
}
