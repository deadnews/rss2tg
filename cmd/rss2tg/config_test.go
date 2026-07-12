package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name    string
		envVars map[string]string
		wantErr bool
		check   func(t *testing.T, cfg *Config)
	}{
		{
			name:    "missing RSS2TG_TOKEN",
			envVars: map[string]string{},
			wantErr: true,
		},
		{
			name: "missing RSS2TG_MANAGER",
			envVars: map[string]string{
				"RSS2TG_TOKEN": "123:abc",
			},
			wantErr: true,
		},
		{
			name: "invalid RSS2TG_MANAGER",
			envVars: map[string]string{
				"RSS2TG_TOKEN":   "123:abc",
				"RSS2TG_MANAGER": "notanumber",
			},
			wantErr: true,
		},
		{
			name: "invalid RSS2TG_INTERVAL",
			envVars: map[string]string{
				"RSS2TG_TOKEN":    "123:abc",
				"RSS2TG_MANAGER":  "12345",
				"RSS2TG_INTERVAL": "badvalue",
			},
			wantErr: true,
		},
		{
			name: "zero RSS2TG_INTERVAL",
			envVars: map[string]string{
				"RSS2TG_TOKEN":    "123:abc",
				"RSS2TG_MANAGER":  "12345",
				"RSS2TG_INTERVAL": "0s",
			},
			wantErr: true,
		},
		{
			name: "negative RSS2TG_INTERVAL",
			envVars: map[string]string{
				"RSS2TG_TOKEN":    "123:abc",
				"RSS2TG_MANAGER":  "12345",
				"RSS2TG_INTERVAL": "-1m",
			},
			wantErr: true,
		},
		{
			name: "defaults",
			envVars: map[string]string{
				"RSS2TG_TOKEN":   "123:abc",
				"RSS2TG_MANAGER": "12345",
			},
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "123:abc", cfg.BotToken)
				assert.Equal(t, int64(12345), cfg.Manager)
				assert.Equal(t, 10*time.Minute, cfg.Interval)
				assert.Equal(t, "rss2tg.db", cfg.DBPath)
			},
		},
		{
			name: "custom values",
			envVars: map[string]string{
				"RSS2TG_TOKEN":    "456:def",
				"RSS2TG_MANAGER":  "67890",
				"RSS2TG_INTERVAL": "10m",
				"RSS2TG_DB_PATH":  "/data/bot.db",
			},
			check: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, "456:def", cfg.BotToken)
				assert.Equal(t, int64(67890), cfg.Manager)
				assert.Equal(t, 10*time.Minute, cfg.Interval)
				assert.Equal(t, "/data/bot.db", cfg.DBPath)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("RSS2TG_TOKEN", "")
			t.Setenv("RSS2TG_MANAGER", "")
			t.Setenv("RSS2TG_INTERVAL", "")
			t.Setenv("RSS2TG_DB_PATH", "")

			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			cfg, err := LoadConfig()

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
