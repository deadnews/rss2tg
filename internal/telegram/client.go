// Package telegram is a Telegram Bot API client.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// DefaultBaseURL is the production Telegram Bot API endpoint.
const DefaultBaseURL = "https://api.telegram.org"

const longPollTimeout = 30

// Client is a Telegram Bot API client.
type Client struct {
	token   string
	BaseURL string
	client  *http.Client
}

// NewClient creates a new Telegram API client.
func NewClient(token string) *Client {
	return &Client{
		token:   token,
		BaseURL: DefaultBaseURL,
		client: &http.Client{
			Timeout: (longPollTimeout + 5) * time.Second,
		},
	}
}

// GetMe validates the bot token.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var resp Response[User]
	if err := c.get(ctx, "getMe", nil, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("getMe: %s", resp.Desc)
	}
	return &resp.Result, nil
}

// GetUpdates long-polls for new updates starting from offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	query := url.Values{
		"offset":  {strconv.FormatInt(offset, 10)},
		"timeout": {strconv.Itoa(longPollTimeout)},
	}
	var resp Response[[]Update]
	if err := c.get(ctx, "getUpdates", query, &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("getUpdates: %s", resp.Desc)
	}
	return resp.Result, nil
}

// SendMessage sends an HTML message to a chat, retrying once on rate limit.
func (c *Client) SendMessage(ctx context.Context, chatID int64, threadID int, text string, disablePreview bool) error {
	payload := sendMessageRequest{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Text:            text,
		ParseMode:       "HTML",
	}
	if disablePreview {
		payload.LinkPreviewOptions = &linkPreviewOptions{IsDisabled: true}
	}
	return c.postWithRetry(ctx, "sendMessage", payload)
}

// SendPhoto sends a photo by URL with an HTML caption.
func (c *Client) SendPhoto(ctx context.Context, chatID int64, threadID int, photoURL, caption string) error {
	payload := sendPhotoRequest{
		ChatID:          chatID,
		MessageThreadID: threadID,
		Photo:           photoURL,
		Caption:         caption,
		ParseMode:       "HTML",
	}
	return c.postWithRetry(ctx, "sendPhoto", payload)
}

// maxForumTopicName is Telegram's forum topic name length limit.
const maxForumTopicName = 128

// CreateForumTopic creates a forum topic and returns its message thread ID.
func (c *Client) CreateForumTopic(ctx context.Context, chatID int64, name string) (int, error) {
	if r := []rune(name); len(r) > maxForumTopicName {
		name = string(r[:maxForumTopicName])
	}
	body, err := json.Marshal(createForumTopicRequest{ChatID: chatID, Name: name})
	if err != nil {
		return 0, fmt.Errorf("createForumTopic marshal: %w", err)
	}
	result, err := c.post(ctx, "createForumTopic", body)
	if err != nil {
		return 0, err
	}
	if !result.OK {
		return 0, fmt.Errorf("createForumTopic: %s", result.Desc)
	}
	var topic ForumTopic
	if err := json.Unmarshal(result.Result, &topic); err != nil {
		return 0, fmt.Errorf("createForumTopic decode: %w", err)
	}
	return topic.MessageThreadID, nil
}

// APIError is a permanent Telegram rejection.
type APIError struct {
	Method string
	Desc   string
}

func (e *APIError) Error() string {
	return e.Method + ": " + e.Desc
}

// postWithRetry POSTs payload, retrying once on 429; a permanent rejection returns *APIError.
func (c *Client) postWithRetry(ctx context.Context, method string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("%s marshal: %w", method, err)
	}

	result, err := c.post(ctx, method, body)
	if err != nil {
		return err
	}
	if result.OK {
		return nil
	}

	if result.Parameters != nil && result.Parameters.RetryAfter > 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: %w", method, ctx.Err())
		case <-time.After(time.Duration(result.Parameters.RetryAfter) * time.Second):
		}

		result, err = c.post(ctx, method, body)
		if err != nil {
			return err
		}
		if result.OK {
			return nil
		}
		if result.Parameters != nil && result.Parameters.RetryAfter > 0 {
			return fmt.Errorf("%s: %s", method, result.Desc)
		}
	}

	return &APIError{Method: method, Desc: result.Desc}
}

func (c *Client) post(ctx context.Context, method string, body []byte) (*Response[json.RawMessage], error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(method), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	var result Response[json.RawMessage]
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("%s decode (status %d): %w", method, resp.StatusCode, err)
	}
	return &result, nil
}

func (c *Client) get(ctx context.Context, method string, query url.Values, dest any) error {
	endpoint := c.url(method)
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, http.NoBody)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("%s decode (status %d): %w", method, resp.StatusCode, err)
	}
	return nil
}

func (c *Client) url(method string) string {
	return c.BaseURL + "/bot" + c.token + "/" + method
}
