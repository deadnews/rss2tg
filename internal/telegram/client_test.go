package telegram

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRedactsBotTokenOnTransportError(t *testing.T) {
	// Point at a dead listener so http.Client.Do fails with the token-bearing URL.
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := ts.URL
	ts.Close()

	const token = "super-secret-token"
	c := NewClient(token)
	c.BaseURL = deadURL

	t.Run("get path", func(t *testing.T) {
		_, err := c.GetMe(t.Context())
		require.Error(t, err)
		assert.NotContains(t, err.Error(), token)
	})

	t.Run("post path", func(t *testing.T) {
		err := c.SendMessage(t.Context(), 100, 0, "hi", false)
		require.Error(t, err)
		assert.NotContains(t, err.Error(), token)
	})
}

func TestGetMe(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/bottest-token/getMe")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[User]{
				OK:     true,
				Result: User{ID: 123, Username: "testbot"},
			})
		})

		user, err := c.GetMe(t.Context())
		require.NoError(t, err)
		assert.Equal(t, int64(123), user.ID)
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
						From: &User{ID: 42},
						Chat: Chat{ID: 42},
						Text: "/help",
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

func TestIsChatAdmin(t *testing.T) {
	statuses := map[string]bool{
		"creator":       true,
		"administrator": true,
		"member":        false,
		"left":          false,
		"kicked":        false,
	}
	for status, want := range statuses {
		t.Run(status, func(t *testing.T) {
			c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Contains(t, r.URL.Path, "/getChatMember")
				assert.Equal(t, "100", r.URL.Query().Get("chat_id"))
				assert.Equal(t, "42", r.URL.Query().Get("user_id"))
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(Response[ChatMember]{OK: true, Result: ChatMember{Status: status}})
			})

			admin, err := c.IsChatAdmin(t.Context(), 100, 42)
			require.NoError(t, err)
			assert.Equal(t, want, admin)
		})
	}

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[ChatMember]{OK: false, Desc: "user not found"})
		})

		_, err := c.IsChatAdmin(t.Context(), 100, 42)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "user not found")
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
			assert.Equal(t, 0, payload.MessageThreadID)
			assert.Equal(t, "HTML", payload.ParseMode)
			assert.Equal(t, "<b>hello</b>", payload.Text)
			assert.Nil(t, payload.LinkPreviewOptions)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.SendMessage(t.Context(), 100, 0, "<b>hello</b>", false)
		require.NoError(t, err)
	})

	t.Run("routes to forum topic", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			var payload sendMessageRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			assert.Equal(t, 7, payload.MessageThreadID)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.SendMessage(t.Context(), 100, 7, "text", false)
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

		err := c.SendMessage(t.Context(), 100, 0, "text", true)
		require.NoError(t, err)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: false, Desc: "chat not found"})
		})

		err := c.SendMessage(t.Context(), 100, 0, "text", false)
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

		err := c.SendMessage(t.Context(), 100, 0, "text", false)
		require.NoError(t, err)
		assert.Equal(t, int32(2), attempts.Load())
	})

	t.Run("rejection is a permanent APIError", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{
				OK: false, Desc: "Bad Request: can't parse entities",
			})
		})

		err := c.SendMessage(t.Context(), 100, 0, "<b>bad", false)
		var apiErr *APIError
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, "sendMessage", apiErr.Method)
	})

	t.Run("persistent rate limit stays retryable", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":          false,
				"description": "Too Many Requests: retry after 1",
				"parameters":  map[string]any{"retry_after": 1},
			})
		})

		err := c.SendMessage(t.Context(), 100, 0, "text", false)
		require.Error(t, err)
		var apiErr *APIError
		assert.NotErrorAs(t, err, &apiErr, "persistent rate limit must not be a permanent APIError")
	})
}

func TestCreateForumTopic(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/createForumTopic")

			var payload createForumTopicRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			assert.Equal(t, int64(100), payload.ChatID)
			assert.Equal(t, "News", payload.Name)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[ForumTopic]{
				OK:     true,
				Result: ForumTopic{MessageThreadID: 55, Name: "News"},
			})
		})

		id, err := c.CreateForumTopic(t.Context(), 100, "News")
		require.NoError(t, err)
		assert.Equal(t, 55, id)
	})

	t.Run("truncates long name", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			var payload createForumTopicRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			assert.Len(t, []rune(payload.Name), maxForumTopicName)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[ForumTopic]{OK: true, Result: ForumTopic{MessageThreadID: 1}})
		})

		_, err := c.CreateForumTopic(t.Context(), 100, strings.Repeat("x", 200))
		require.NoError(t, err)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: false, Desc: "not enough rights"})
		})

		_, err := c.CreateForumTopic(t.Context(), 100, "News")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not enough rights")
	})
}

func TestUnpinAllForumTopicMessages(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Contains(t, r.URL.Path, "/unpinAllForumTopicMessages")

			var payload unpinAllForumTopicMessagesRequest
			_ = json.NewDecoder(r.Body).Decode(&payload)
			assert.Equal(t, int64(100), payload.ChatID)
			assert.Equal(t, 7, payload.MessageThreadID)

			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: true})
		})

		err := c.UnpinAllForumTopicMessages(t.Context(), 100, 7)
		require.NoError(t, err)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: false, Desc: "not enough rights"})
		})

		err := c.UnpinAllForumTopicMessages(t.Context(), 100, 7)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not enough rights")
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

		err := c.SendPhoto(t.Context(), 100, 0, "https://img.com/1.jpg", "<b>caption</b>")
		require.NoError(t, err)
	})

	t.Run("error response", func(t *testing.T) {
		c := testClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(Response[json.RawMessage]{OK: false, Desc: "photo too large"})
		})

		err := c.SendPhoto(t.Context(), 100, 0, "https://img.com/big.jpg", "cap")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "photo too large")
	})
}
