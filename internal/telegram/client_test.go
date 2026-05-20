package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	c := NewClient("test-token")
	c.BaseURL = ts.URL
	return c
}

func TestGetMe(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/bottest-token/getMe")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[User]{
				OK:     true,
				Result: User{ID: 123, IsBot: true, Username: "testbot"},
			})
		})

		user, err := c.GetMe(t.Context())
		require.NoError(t, err)
		assert.Equal(t, int64(123), user.ID)
		assert.True(t, user.IsBot)
		assert.Equal(t, "testbot", user.Username)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[User]{
				OK:   false,
				Desc: "Unauthorized",
			})
		})

		_, err := c.GetMe(t.Context())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Unauthorized")
	})
}

func TestGetUpdates(t *testing.T) {
	t.Run("returns updates", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/getUpdates")
			assert.Equal(t, "1", r.URL.Query().Get("offset"))
			assert.Equal(t, "30", r.URL.Query().Get("timeout"))

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[[]Update]{
				OK: true,
				Result: []Update{
					{UpdateID: 1, Message: &Message{
						MessageID: 10,
						From:      &User{ID: 42},
						Chat:      Chat{ID: 42},
						Text:      "/help",
					}},
				},
			})
		})

		updates, err := c.GetUpdates(t.Context(), 1)
		require.NoError(t, err)
		require.Len(t, updates, 1)
		assert.Equal(t, int64(1), updates[0].UpdateID)
		assert.Equal(t, "/help", updates[0].Message.Text)
	})

	t.Run("empty updates", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[[]Update]{OK: true, Result: []Update{}})
		})

		updates, err := c.GetUpdates(t.Context(), 0)
		require.NoError(t, err)
		assert.Empty(t, updates)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[[]Update]{OK: false, Desc: "conflict"})
		})

		_, err := c.GetUpdates(t.Context(), 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "conflict")
	})
}

func TestSendMessage(t *testing.T) {
	t.Run("success with preview", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/sendMessage")
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

			var payload sendMessageRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			assert.Equal(t, int64(100), payload.ChatID)
			assert.Equal(t, "HTML", payload.ParseMode)
			assert.Equal(t, "<b>hello</b>", payload.Text)
			assert.Nil(t, payload.LinkPreviewOptions)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.SendMessage(t.Context(), 100, "<b>hello</b>", false)
		require.NoError(t, err)
	})

	t.Run("success with disabled preview", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			var payload sendMessageRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if assert.NotNil(t, payload.LinkPreviewOptions) {
				assert.True(t, payload.LinkPreviewOptions.IsDisabled)
			}

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.SendMessage(t.Context(), 100, "text", true)
		require.NoError(t, err)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: false, Desc: "chat not found"})
		})

		err := c.SendMessage(t.Context(), 100, "text", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chat not found")
	})

	t.Run("retries on rate limit", func(t *testing.T) {
		var attempts atomic.Int32
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if attempts.Add(1) == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":          false,
					"description": "Too Many Requests: retry after 1",
					"parameters":  map[string]any{"retry_after": 1},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.SendMessage(t.Context(), 100, "text", false)
		require.NoError(t, err)
		assert.Equal(t, int32(2), attempts.Load())
	})
}

func TestSendPhoto(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/sendPhoto")

			var payload sendPhotoRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			assert.Equal(t, int64(100), payload.ChatID)
			assert.Equal(t, "https://img.com/1.jpg", payload.Photo)
			assert.Equal(t, "<b>caption</b>", payload.Caption)
			assert.Equal(t, "HTML", payload.ParseMode)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.SendPhoto(t.Context(), 100, "https://img.com/1.jpg", "<b>caption</b>")
		require.NoError(t, err)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: false, Desc: "photo too large"})
		})

		err := c.SendPhoto(t.Context(), 100, "https://img.com/big.jpg", "cap")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "photo too large")
	})
}
