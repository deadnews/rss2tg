// Package store provides bbolt-backed persistence for subscriptions and seen-entry tracking.
package store

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"time"

	bolt "go.etcd.io/bbolt"
)

const seenRetention = 7 * 24 * time.Hour

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
	URL    string `json:"-"`
	Title  string `json:"title,omitempty"`
	Format string `json:"format"`
	Shorts bool   `json:"shorts,omitempty"`
}

func decodeSub(url string, v []byte) (Sub, error) {
	sub := Sub{URL: url}
	if err := json.Unmarshal(v, &sub); err != nil {
		return Sub{}, fmt.Errorf("decoding sub %q: %w", url, err)
	}
	return sub, nil
}

// New opens a bbolt database and creates top-level buckets.
func New(path string) (*Store, error) {
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 1 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	err = db.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists(bucketSubs); err != nil {
			return fmt.Errorf("creating subs bucket: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists(bucketSeen); err != nil {
			return fmt.Errorf("creating seen bucket: %w", err)
		}
		return nil
	})
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initializing buckets: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("closing database: %w", err)
	}
	return nil
}

// AddSub subscribes a chat to a feed.
func (s *Store) AddSub(chatID int64, sub Sub) error {
	val, err := json.Marshal(sub)
	if err != nil {
		return fmt.Errorf("encoding sub: %w", err)
	}
	err = s.db.Update(func(tx *bolt.Tx) error {
		chat, err := tx.Bucket(bucketSubs).CreateBucketIfNotExists(chatKey(chatID))
		if err != nil {
			return fmt.Errorf("creating chat bucket: %w", err)
		}
		return chat.Put([]byte(sub.URL), val)
	})
	if err != nil {
		return fmt.Errorf("adding subscription: %w", err)
	}
	return nil
}

// RemoveSub unsubscribes a chat from a feed URL. Returns true if it existed.
func (s *Store) RemoveSub(chatID int64, feedURL string) (bool, error) {
	var existed bool

	err := s.db.Update(func(tx *bolt.Tx) error {
		chat := tx.Bucket(bucketSubs).Bucket(chatKey(chatID))
		if chat == nil {
			return nil
		}
		if chat.Get([]byte(feedURL)) != nil {
			existed = true
		}
		return chat.Delete([]byte(feedURL))
	})
	if err != nil {
		return false, fmt.Errorf("removing subscription: %w", err)
	}
	return existed, nil
}

// ListSubs returns all subscriptions for a chat.
func (s *Store) ListSubs(chatID int64) ([]Sub, error) {
	var subs []Sub

	err := s.db.View(func(tx *bolt.Tx) error {
		chat := tx.Bucket(bucketSubs).Bucket(chatKey(chatID))
		if chat == nil {
			return nil
		}
		return chat.ForEach(func(k, v []byte) error {
			sub, err := decodeSub(string(k), v)
			if err != nil {
				return err
			}
			subs = append(subs, sub)
			return nil
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing subscriptions: %w", err)
	}

	return subs, nil
}

// SetFormat updates the format for all subscriptions in a chat, preserving other options.
func (s *Store) SetFormat(chatID int64, format string) (int, error) {
	var count int

	err := s.db.Update(func(tx *bolt.Tx) error {
		chat := tx.Bucket(bucketSubs).Bucket(chatKey(chatID))
		if chat == nil {
			return nil
		}
		// bbolt: ForEach must not modify the bucket; collect entries first.
		var subs []Sub
		if err := chat.ForEach(func(k, v []byte) error {
			sub, err := decodeSub(string(k), v)
			if err != nil {
				return err
			}
			subs = append(subs, sub)
			return nil
		}); err != nil {
			return fmt.Errorf("collecting subscriptions: %w", err)
		}
		count = len(subs)
		for _, sub := range subs {
			sub.Format = format
			val, err := json.Marshal(sub)
			if err != nil {
				return fmt.Errorf("encoding sub: %w", err)
			}
			if err := chat.Put([]byte(sub.URL), val); err != nil {
				return fmt.Errorf("writing subscription: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("setting format: %w", err)
	}

	return count, nil
}

// ChatFeed pairs a chat ID with its subscription options for a given feed.
type ChatFeed struct {
	ChatID int64
	Format string
	Shorts bool
}

// AllFeeds returns a map of feed URL → list of subscribed chats with their options.
func (s *Store) AllFeeds() (map[string][]ChatFeed, error) {
	feeds := make(map[string][]ChatFeed)

	err := s.db.View(func(tx *bolt.Tx) error {
		subs := tx.Bucket(bucketSubs)
		return subs.ForEach(func(k, _ []byte) error {
			chatID := parseChatKey(k)
			chat := subs.Bucket(k)
			if chat == nil {
				return nil
			}
			return chat.ForEach(func(url, v []byte) error {
				sub, err := decodeSub(string(url), v)
				if err != nil {
					return err
				}
				feeds[sub.URL] = append(feeds[sub.URL], ChatFeed{
					ChatID: chatID,
					Format: sub.Format,
					Shorts: sub.Shorts,
				})
				return nil
			})
		})
	})
	if err != nil {
		return nil, fmt.Errorf("listing all feeds: %w", err)
	}

	return feeds, nil
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
		return false, fmt.Errorf("checking seen: %w", err)
	}

	return seen, nil
}

// MarkSeen marks an entry GUID as seen for a feed URL with current timestamp.
func (s *Store) MarkSeen(feedURL, guid string) error {
	err := s.db.Update(func(tx *bolt.Tx) error {
		feed, err := tx.Bucket(bucketSeen).CreateBucketIfNotExists([]byte(feedURL))
		if err != nil {
			return fmt.Errorf("creating seen feed bucket: %w", err)
		}
		ts := make([]byte, 8)
		binary.BigEndian.PutUint64(ts, uint64(time.Now().Unix())) //nolint:gosec // unix timestamps are always positive
		return feed.Put([]byte(guid), ts)
	})
	if err != nil {
		return fmt.Errorf("marking seen: %w", err)
	}
	return nil
}

// CleanSeen removes seen entries older than seenRetention.
func (s *Store) CleanSeen() error {
	cutoff := uint64(time.Now().Add(-seenRetention).Unix()) //nolint:gosec // unix timestamps are always positive

	err := s.db.Update(func(tx *bolt.Tx) error {
		seen := tx.Bucket(bucketSeen)
		return seen.ForEach(func(feedKey, _ []byte) error {
			feed := seen.Bucket(feedKey)
			if feed == nil {
				return nil
			}

			var toDelete [][]byte
			if err := feed.ForEach(func(k, v []byte) error {
				if len(v) == 8 && binary.BigEndian.Uint64(v) < cutoff {
					toDelete = append(toDelete, k)
				}
				return nil
			}); err != nil {
				return fmt.Errorf("iterating seen entries: %w", err)
			}

			for _, k := range toDelete {
				if err := feed.Delete(k); err != nil {
					return fmt.Errorf("deleting seen entry: %w", err)
				}
			}
			return nil
		})
	})
	if err != nil {
		return fmt.Errorf("cleaning seen entries: %w", err)
	}
	return nil
}

func chatKey(id int64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(id)) //nolint:gosec // chat IDs fit in int64
	return b
}

func parseChatKey(b []byte) int64 {
	return int64(binary.BigEndian.Uint64(b)) //nolint:gosec // chat IDs fit in int64
}
