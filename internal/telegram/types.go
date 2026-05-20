package telegram

// User represents a Telegram user.
type User struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID int64 `json:"id"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID int64  `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// Update represents a Telegram update.
type Update struct {
	UpdateID    int64    `json:"update_id"`
	Message     *Message `json:"message"`
	ChannelPost *Message `json:"channel_post"`
}

type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

// Response is a generic Telegram Bot API response envelope.
type Response[T any] struct {
	OK         bool                `json:"ok"`
	Result     T                   `json:"result"`
	Desc       string              `json:"description"`
	Parameters *responseParameters `json:"parameters,omitempty"`
}

type responseParameters struct {
	RetryAfter int `json:"retry_after"`
}

type sendMessageRequest struct {
	ChatID             int64               `json:"chat_id"`
	Text               string              `json:"text"`
	ParseMode          string              `json:"parse_mode"`
	LinkPreviewOptions *linkPreviewOptions `json:"link_preview_options,omitempty"`
}

type sendPhotoRequest struct {
	ChatID    int64  `json:"chat_id"`
	Photo     string `json:"photo"`
	Caption   string `json:"caption,omitempty"`
	ParseMode string `json:"parse_mode"`
}
