// Package telegram is a Telegram Bot API client.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// DefaultBaseURL is the production Telegram Bot API endpoint.
const DefaultBaseURL = "https://api.telegram.org"

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
			Timeout: 35 * time.Second,
		},
	}
}

// GetMe validates the bot token.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var resp Response[User]
	if err := c.get(ctx, "getMe", "", &resp); err != nil {
		return nil, err
	}
	if !resp.OK {
		return nil, fmt.Errorf("getMe: %s", resp.Desc)
	}
	return &resp.Result, nil
}

// GetUpdates long-polls for new updates starting from offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	query := "?offset=" + strconv.FormatInt(offset, 10) + "&timeout=30"
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
func (c *Client) SendMessage(ctx context.Context, chatID int64, text string, disablePreview bool) error {
	payload := sendMessageRequest{
		ChatID:    chatID,
		Text:      text,
		ParseMode: "HTML",
	}
	if disablePreview {
		payload.LinkPreviewOptions = &linkPreviewOptions{IsDisabled: true}
	}
	return c.postWithRetry(ctx, "sendMessage", payload)
}

// SendPhoto sends a photo by URL with an HTML caption.
func (c *Client) SendPhoto(ctx context.Context, chatID int64, photoURL, caption string) error {
	payload := sendPhotoRequest{
		ChatID:    chatID,
		Photo:     photoURL,
		Caption:   caption,
		ParseMode: "HTML",
	}
	return c.postWithRetry(ctx, "sendPhoto", payload)
}

// postWithRetry POSTs payload, retrying once on 429.
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
		timer := time.NewTimer(time.Duration(result.Parameters.RetryAfter) * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%s: %w", method, ctx.Err())
		case <-timer.C:
		}

		result, err = c.post(ctx, method, body)
		if err != nil {
			return err
		}
		if result.OK {
			return nil
		}
	}

	return fmt.Errorf("%s: %s", method, result.Desc)
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
		return nil, fmt.Errorf("%s decode: %w", method, err)
	}
	return &result, nil
}

func (c *Client) get(ctx context.Context, method, query string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(method)+query, http.NoBody)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return nil
}

func (c *Client) url(method string) string {
	return c.BaseURL + "/bot" + c.token + "/" + method
}
