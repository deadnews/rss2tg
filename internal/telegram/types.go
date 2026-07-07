package telegram

// User represents a Telegram user.
type User struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID      int64  `json:"id"`
	Type    string `json:"type"`
	Title   string `json:"title"`
	IsForum bool   `json:"is_forum"`
}

// ForumTopic represents a created forum topic.
type ForumTopic struct {
	MessageThreadID int    `json:"message_thread_id"`
	Name            string `json:"name"`
}

// ForumTopicCreated is the service payload of a topic-creation message.
type ForumTopicCreated struct {
	Name string `json:"name"`
}

// ChatMember reports a user's membership status in a chat.
type ChatMember struct {
	Status string `json:"status"`
}

// Message represents a Telegram message.
type Message struct {
	From              *User              `json:"from"`
	Chat              Chat               `json:"chat"`
	Text              string             `json:"text"`
	MessageThreadID   int                `json:"message_thread_id"`
	IsTopicMessage    bool               `json:"is_topic_message"`
	ForumTopicCreated *ForumTopicCreated `json:"forum_topic_created"`
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
	MessageThreadID    int                 `json:"message_thread_id,omitempty"`
	Text               string              `json:"text"`
	ParseMode          string              `json:"parse_mode"`
	LinkPreviewOptions *linkPreviewOptions `json:"link_preview_options,omitempty"`
}

type sendPhotoRequest struct {
	ChatID          int64  `json:"chat_id"`
	MessageThreadID int    `json:"message_thread_id,omitempty"`
	Photo           string `json:"photo"`
	Caption         string `json:"caption,omitempty"`
	ParseMode       string `json:"parse_mode"`
}

type createForumTopicRequest struct {
	ChatID int64  `json:"chat_id"`
	Name   string `json:"name"`
}

type unpinAllForumTopicMessagesRequest struct {
	ChatID          int64 `json:"chat_id"`
	MessageThreadID int   `json:"message_thread_id"`
}
