// Package store provides bbolt-backed persistence for subscriptions and seen-entry tracking.
package store

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bucketSubs = []byte("subs")
	bucketSeen = []byte("seen")
)

// Store wraps bbolt for subscription and seen-entry tracking.
type Store struct {
	db *bolt.DB
}

// Sub represents a feed subscription.
type Sub struct {
	URL     string   `json:"-"`
	Title   string   `json:"title,omitempty"`
	Format  string   `json:"format"`
	Shorts  bool     `json:"shorts,omitempty"`
	NoLive  bool     `json:"nolive,omitempty"`
	Exclude []string `json:"exclude,omitempty"`
	Include []string `json:"include,omitempty"`
}

func decodeSub(url string, v []byte) (Sub, error) {
	sub := Sub{URL: url}
	if err := json.Unmarshal(v, &sub); err != nil {
		return Sub{}, fmt.Errorf("decode sub %q: %w", url, err)
	}
	return sub, nil
}

// collectSubs decodes every subscription in a chat bucket.
func collectSubs(chat *bolt.Bucket) ([]Sub, error) {
	var subs []Sub
	err := chat.ForEach(func(k, v []byte) error {
		sub, err := decodeSub(string(k), v)
		if err != nil {
			return err
		}
		subs = append(subs, sub)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("iterate subscriptions: %w", err)
	}
	return subs, nil
}

// New opens a bbolt database and creates top-level buckets.
func New(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketSubs); err != nil {
			return fmt.Errorf("create subs bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSeen); err != nil {
			return fmt.Errorf("create seen bucket: %w", err)
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("init buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// AddSub subscribes a chat topic to a feed. Returns whether the URL was already subscribed.
func (s *Store) AddSub(chatID int64, threadID int, sub *Sub) (bool, error) {
	val, err := json.Marshal(sub)
	if err != nil {
		return false, fmt.Errorf("encode sub: %w", err)
	}
	var existed bool
	err = s.db.Update(func(tx *bolt.Tx) error {
		chat, err := tx.Bucket(bucketSubs).CreateBucketIfNotExists(chatKey(chatID, threadID))
		if err != nil {
			return fmt.Errorf("create chat bucket: %w", err)
		}
		existed = chat.Get([]byte(sub.URL)) != nil
		return chat.Put([]byte(sub.URL), val)
	})
	if err != nil {
		return false, fmt.Errorf("add subscription: %w", err)
	}
	return existed, nil
}

// RemoveSub unsubscribes a chat topic from a feed URL. Returns true if it existed.
func (s *Store) RemoveSub(chatID int64, threadID int, feedURL string) (bool, error) {
	var existed bool

	err := s.db.Update(func(tx *bolt.Tx) error {
		chat := tx.Bucket(bucketSubs).Bucket(chatKey(chatID, threadID))
		if chat == nil {
			return nil
		}
		if chat.Get([]byte(feedURL)) != nil {
			existed = true
		}
		return chat.Delete([]byte(feedURL))
	})
	if err != nil {
		return false, fmt.Errorf("remove subscription: %w", err)
	}
	return existed, nil
}

// ListSubs returns all subscriptions for a chat topic.
func (s *Store) ListSubs(chatID int64, threadID int) ([]Sub, error) {
	var subs []Sub

	err := s.db.View(func(tx *bolt.Tx) error {
		chat := tx.Bucket(bucketSubs).Bucket(chatKey(chatID, threadID))
		if chat == nil {
			return nil
		}
		var err error
		subs, err = collectSubs(chat)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}

	return subs, nil
}

// ChatFeed pairs a chat topic with the subscription it delivers.
type ChatFeed struct {
	ChatID   int64
	ThreadID int
	Sub
}

// ChatFeed returns the sub paired with the given chat topic.
func (sub *Sub) ChatFeed(chatID int64, threadID int) ChatFeed {
	return ChatFeed{ChatID: chatID, ThreadID: threadID, Sub: *sub}
}

// AllSubs returns every subscription across all chats and topics.
func (s *Store) AllSubs() ([]ChatFeed, error) {
	var subs []ChatFeed

	err := s.db.View(func(tx *bolt.Tx) error {
		buckets := tx.Bucket(bucketSubs)
		return buckets.ForEach(func(k, _ []byte) error {
			chatID, threadID := parseChatKey(k)
			chat := buckets.Bucket(k)
			if chat == nil {
				return nil
			}
			return chat.ForEach(func(url, v []byte) error {
				sub, err := decodeSub(string(url), v)
				if err != nil {
					return err
				}
				subs = append(subs, sub.ChatFeed(chatID, threadID))
				return nil
			})
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list all subscriptions: %w", err)
	}

	return subs, nil
}

// AllFeeds returns a map of feed URL → list of subscribed chats with their options.
func (s *Store) AllFeeds() (map[string][]ChatFeed, error) {
	subs, err := s.AllSubs()
	if err != nil {
		return nil, err
	}
	feeds := make(map[string][]ChatFeed)
	for _, cf := range subs {
		feeds[cf.URL] = append(feeds[cf.URL], cf)
	}
	return feeds, nil
}

// ChatSubs returns all subscriptions across every topic of a chat.
func (s *Store) ChatSubs(chatID int64) ([]Sub, error) {
	prefix := chatKey(chatID, 0) // shared 8-byte chat prefix across the chat's topics
	var subs []Sub
	err := s.db.View(func(tx *bolt.Tx) error {
		buckets := tx.Bucket(bucketSubs)
		return buckets.ForEach(func(k, _ []byte) error {
			if !bytes.HasPrefix(k, prefix) {
				return nil
			}
			chat := buckets.Bucket(k)
			if chat == nil {
				return nil
			}
			topicSubs, err := collectSubs(chat)
			if err != nil {
				return err
			}
			subs = append(subs, topicSubs...)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("list chat subscriptions: %w", err)
	}
	return subs, nil
}

// FindFeedThread returns the topic a feed is subscribed under in a chat, if any.
func (s *Store) FindFeedThread(chatID int64, feedURL string) (threadID int, found bool, err error) {
	prefix := chatKey(chatID, 0) // shared 8-byte chat prefix across the chat's topics
	err = s.db.View(func(tx *bolt.Tx) error {
		subs := tx.Bucket(bucketSubs)
		return subs.ForEach(func(k, _ []byte) error {
			if found || !bytes.HasPrefix(k, prefix) {
				return nil
			}
			if chat := subs.Bucket(k); chat != nil && chat.Get([]byte(feedURL)) != nil {
				_, threadID = parseChatKey(k)
				found = true
			}
			return nil
		})
	})
	if err != nil {
		return 0, false, fmt.Errorf("find feed thread: %w", err)
	}
	return threadID, found, nil
}

// IsSeen checks if an entry GUID has been seen for a feed URL.
func (s *Store) IsSeen(feedURL, guid string) (bool, error) {
	var seen bool

	err := s.db.View(func(tx *bolt.Tx) error {
		feed := tx.Bucket(bucketSeen).Bucket([]byte(feedURL))
		if feed == nil {
			return nil
		}
		seen = feed.Get([]byte(guid)) != nil
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("check seen: %w", err)
	}

	return seen, nil
}

// MarkSeen marks an entry GUID as seen.
func (s *Store) MarkSeen(feedURL, guid string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		feed, err := tx.Bucket(bucketSeen).CreateBucketIfNotExists([]byte(feedURL))
		if err != nil {
			return fmt.Errorf("create seen feed bucket: %w", err)
		}
		ts := make([]byte, 8)
		binary.BigEndian.PutUint64(ts, uint64(time.Now().Unix())) //nolint:gosec // G115
		return feed.Put([]byte(guid), ts)
	})
	if err != nil {
		return fmt.Errorf("mark seen: %w", err)
	}
	return nil
}

// TrimSeen keeps a feed's newest keep entries by mark time.
func (s *Store) TrimSeen(feedURL string, keep int) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		feed := tx.Bucket(bucketSeen).Bucket([]byte(feedURL))
		if feed == nil {
			return nil
		}

		type entry struct {
			ts  uint64
			key []byte
		}
		var entries []entry
		if err := feed.ForEach(func(k, v []byte) error {
			entries = append(entries, entry{binary.BigEndian.Uint64(v), bytes.Clone(k)})
			return nil
		}); err != nil {
			return fmt.Errorf("iterate seen entries: %w", err)
		}
		if len(entries) <= keep {
			return nil
		}

		slices.SortFunc(entries, func(a, b entry) int { return cmp.Compare(a.ts, b.ts) })
		for _, e := range entries[:len(entries)-keep] {
			if err := feed.Delete(e.key); err != nil {
				return fmt.Errorf("delete seen entry: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("trim seen entries: %w", err)
	}
	return nil
}

// chatKey encodes a chat topic; threadID 0 uses a legacy 8-byte chat-only key.
func chatKey(chatID int64, threadID int) []byte {
	if threadID == 0 {
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(chatID)) //nolint:gosec // G115
		return b
	}
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[:8], uint64(chatID))   //nolint:gosec // G115
	binary.BigEndian.PutUint64(b[8:], uint64(threadID)) //nolint:gosec // G115
	return b
}

func parseChatKey(b []byte) (chatID int64, threadID int) {
	chatID = int64(binary.BigEndian.Uint64(b[:8])) //nolint:gosec // G115
	if len(b) >= 16 {
		threadID = int(binary.BigEndian.Uint64(b[8:])) //nolint:gosec // G115
	}
	return chatID, threadID
}
