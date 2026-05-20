package store

import (
	"encoding/binary"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	bolt "go.etcd.io/bbolt"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestNew(t *testing.T) {
	t.Run("opens database", func(t *testing.T) {
		s := testStore(t)
		assert.NotNil(t, s)
	})

	t.Run("invalid path", func(t *testing.T) {
		_, err := New("/nonexistent/dir/test.db")
		require.Error(t, err)
	})
}

func TestAddSubAndListSubs(t *testing.T) {
	s := testStore(t)

	err := s.AddSub(100, Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)

	err = s.AddSub(100, Sub{URL: "https://example.com/feed2.xml", Format: "pw"})
	require.NoError(t, err)

	subs, err := s.ListSubs(100)
	require.NoError(t, err)
	assert.Len(t, subs, 2)
}

func TestListSubsEmpty(t *testing.T) {
	s := testStore(t)

	subs, err := s.ListSubs(999)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestRemoveSub(t *testing.T) {
	s := testStore(t)

	err := s.AddSub(100, Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)

	existed, err := s.RemoveSub(100, "https://example.com/feed.xml")
	require.NoError(t, err)
	assert.True(t, existed)

	existed, err = s.RemoveSub(100, "https://example.com/feed.xml")
	require.NoError(t, err)
	assert.False(t, existed)

	existed, err = s.RemoveSub(999, "https://example.com/nope.xml")
	require.NoError(t, err)
	assert.False(t, existed)
}

func TestSetFormat(t *testing.T) {
	s := testStore(t)

	err := s.AddSub(100, Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	err = s.AddSub(100, Sub{URL: "https://b.com/feed", Format: "link"})
	require.NoError(t, err)

	count, err := s.SetFormat(100, "pw")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	subs, err := s.ListSubs(100)
	require.NoError(t, err)
	for _, sub := range subs {
		assert.Equal(t, "pw", sub.Format)
	}
}

func TestSetFormatNoSubs(t *testing.T) {
	s := testStore(t)

	count, err := s.SetFormat(999, "pw")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestAllFeeds(t *testing.T) {
	s := testStore(t)

	err := s.AddSub(100, Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	err = s.AddSub(200, Sub{URL: "https://a.com/feed", Format: "pw"})
	require.NoError(t, err)
	err = s.AddSub(100, Sub{URL: "https://b.com/feed", Format: "link"})
	require.NoError(t, err)

	feeds, err := s.AllFeeds()
	require.NoError(t, err)
	assert.Len(t, feeds, 2)
	assert.Len(t, feeds["https://a.com/feed"], 2)
	assert.Len(t, feeds["https://b.com/feed"], 1)
}

func TestAddSubRoundTripsShorts(t *testing.T) {
	s := testStore(t)

	err := s.AddSub(100, Sub{URL: "https://yt/feed", Format: "pw", Shorts: true})
	require.NoError(t, err)

	subs, err := s.ListSubs(100)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "pw", subs[0].Format)
	assert.True(t, subs[0].Shorts)
}

func TestSetFormatPreservesShorts(t *testing.T) {
	s := testStore(t)

	err := s.AddSub(100, Sub{URL: "https://yt/feed", Format: "link", Shorts: true})
	require.NoError(t, err)

	_, err = s.SetFormat(100, "pw")
	require.NoError(t, err)

	subs, err := s.ListSubs(100)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "pw", subs[0].Format)
	assert.True(t, subs[0].Shorts, "Shorts flag should be preserved across SetFormat")
}

func TestSeenTracking(t *testing.T) {
	s := testStore(t)

	seen, err := s.IsSeen("https://a.com/feed", "guid-1")
	require.NoError(t, err)
	assert.False(t, seen)

	err = s.MarkSeen("https://a.com/feed", "guid-1")
	require.NoError(t, err)

	seen, err = s.IsSeen("https://a.com/feed", "guid-1")
	require.NoError(t, err)
	assert.True(t, seen)

	seen, err = s.IsSeen("https://a.com/feed", "guid-2")
	require.NoError(t, err)
	assert.False(t, seen)
}

func TestCleanSeenKeepsRecent(t *testing.T) {
	s := testStore(t)

	err := s.MarkSeen("https://a.com/feed", "recent-guid")
	require.NoError(t, err)

	err = s.CleanSeen()
	require.NoError(t, err)

	seen, err := s.IsSeen("https://a.com/feed", "recent-guid")
	require.NoError(t, err)
	assert.True(t, seen)
}

func TestCleanSeenRemovesOld(t *testing.T) {
	s := testStore(t)

	// Write a stale entry directly with an aged timestamp.
	feedURL := "https://a.com/feed"
	require.NoError(t, s.db.Update(func(tx *bolt.Tx) error {
		feed, err := tx.Bucket(bucketSeen).CreateBucketIfNotExists([]byte(feedURL))
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}
		ts := make([]byte, 8)
		binary.BigEndian.PutUint64(ts, uint64(time.Now().Add(-2*seenRetention).Unix()))
		return feed.Put([]byte("old-guid"), ts)
	}))

	require.NoError(t, s.CleanSeen())

	seen, err := s.IsSeen(feedURL, "old-guid")
	require.NoError(t, err)
	assert.False(t, seen)
}
