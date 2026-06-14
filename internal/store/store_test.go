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

	existed, err := s.AddSub(100, 0, &Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)
	assert.False(t, existed)

	_, err = s.AddSub(100, 0, &Sub{URL: "https://example.com/feed2.xml", Format: "pw"})
	require.NoError(t, err)

	existed, err = s.AddSub(100, 0, &Sub{URL: "https://example.com/feed.xml", Format: "pw"})
	require.NoError(t, err)
	assert.True(t, existed, "re-add should report existed=true")

	subs, err := s.ListSubs(100, 0)
	require.NoError(t, err)
	assert.Len(t, subs, 2)
}

func TestAddSubRoundTripsFilters(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 0, &Sub{
		URL:     "https://a.com/feed",
		Format:  "link",
		Exclude: []string{"crypto", "ai"},
		Include: []string{"go"},
	})
	require.NoError(t, err)

	subs, err := s.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, []string{"crypto", "ai"}, subs[0].Exclude)
	assert.Equal(t, []string{"go"}, subs[0].Include)
}

func TestListSubsEmpty(t *testing.T) {
	s := testStore(t)

	subs, err := s.ListSubs(999, 0)
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestRemoveSub(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 0, &Sub{URL: "https://example.com/feed.xml", Format: "link"})
	require.NoError(t, err)

	existed, err := s.RemoveSub(100, 0, "https://example.com/feed.xml")
	require.NoError(t, err)
	assert.True(t, existed)

	existed, err = s.RemoveSub(100, 0, "https://example.com/feed.xml")
	require.NoError(t, err)
	assert.False(t, existed)

	existed, err = s.RemoveSub(999, 0, "https://example.com/nope.xml")
	require.NoError(t, err)
	assert.False(t, existed)
}

func TestSetFormat(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 0, &Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = s.AddSub(100, 0, &Sub{URL: "https://b.com/feed", Format: "link"})
	require.NoError(t, err)

	count, err := s.SetFormat(100, 0, "pw")
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	subs, err := s.ListSubs(100, 0)
	require.NoError(t, err)
	for _, sub := range subs {
		assert.Equal(t, "pw", sub.Format)
	}
}

func TestSetFormatNoSubs(t *testing.T) {
	s := testStore(t)

	count, err := s.SetFormat(999, 0, "pw")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestAllFeeds(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 0, &Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = s.AddSub(200, 0, &Sub{URL: "https://a.com/feed", Format: "pw"})
	require.NoError(t, err)
	_, err = s.AddSub(100, 0, &Sub{URL: "https://b.com/feed", Format: "link"})
	require.NoError(t, err)

	feeds, err := s.AllFeeds()
	require.NoError(t, err)
	assert.Len(t, feeds, 2)
	assert.Len(t, feeds["https://a.com/feed"], 2)
	assert.Len(t, feeds["https://b.com/feed"], 1)
}

func TestSubsIsolatedPerThread(t *testing.T) {
	s := testStore(t)
	const url = "https://a.com/feed"

	_, err := s.AddSub(100, 0, &Sub{URL: url, Format: "link"})
	require.NoError(t, err)
	_, err = s.AddSub(100, 5, &Sub{URL: url, Format: "pw"})
	require.NoError(t, err)

	general, err := s.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, general, 1)
	assert.Equal(t, "link", general[0].Format)

	topic, err := s.ListSubs(100, 5)
	require.NoError(t, err)
	require.Len(t, topic, 1)
	assert.Equal(t, "pw", topic[0].Format)

	// Same feed in two topics → two delivery targets, each with its thread ID.
	feeds, err := s.AllFeeds()
	require.NoError(t, err)
	require.Len(t, feeds[url], 2)
	threads := map[int]bool{}
	for _, cf := range feeds[url] {
		assert.Equal(t, int64(100), cf.ChatID)
		threads[cf.ThreadID] = true
	}
	assert.Equal(t, map[int]bool{0: true, 5: true}, threads)
}

func TestFindFeedThread(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 7, &Sub{URL: "https://a.com/feed", Format: "link"})
	require.NoError(t, err)
	_, err = s.AddSub(100, 0, &Sub{URL: "https://general.com/feed", Format: "link"})
	require.NoError(t, err)

	tid, found, err := s.FindFeedThread(100, "https://a.com/feed")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 7, tid)

	tid, found, err = s.FindFeedThread(100, "https://general.com/feed")
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, 0, tid)

	_, found, err = s.FindFeedThread(100, "https://missing.com/feed")
	require.NoError(t, err)
	assert.False(t, found)

	// A different chat with the same URL must not match.
	_, found, err = s.FindFeedThread(200, "https://a.com/feed")
	require.NoError(t, err)
	assert.False(t, found)
}

func TestAddSubRoundTripsShorts(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 0, &Sub{URL: "https://yt/feed", Format: "pw", Shorts: true})
	require.NoError(t, err)

	subs, err := s.ListSubs(100, 0)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "pw", subs[0].Format)
	assert.True(t, subs[0].Shorts)
}

func TestSetFormatPreservesShorts(t *testing.T) {
	s := testStore(t)

	_, err := s.AddSub(100, 0, &Sub{URL: "https://yt/feed", Format: "link", Shorts: true})
	require.NoError(t, err)

	_, err = s.SetFormat(100, 0, "pw")
	require.NoError(t, err)

	subs, err := s.ListSubs(100, 0)
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

func TestTrimSeenUnderCapKeepsAll(t *testing.T) {
	s := testStore(t)
	feedURL := "https://a.com/feed"

	require.NoError(t, s.MarkSeen(feedURL, "guid-1"))
	require.NoError(t, s.MarkSeen(feedURL, "guid-2"))

	require.NoError(t, s.TrimSeen(feedURL, 5))

	for _, guid := range []string{"guid-1", "guid-2"} {
		seen, err := s.IsSeen(feedURL, guid)
		require.NoError(t, err)
		assert.True(t, seen, guid)
	}
}

func TestTrimSeenEvictsOldestByMarkTime(t *testing.T) {
	s := testStore(t)
	feedURL := "https://a.com/feed"

	// Write entries with explicit ascending mark times; "old" is the oldest.
	for i, guid := range []string{"old", "mid", "new"} {
		require.NoError(t, s.db.Update(func(tx *bolt.Tx) error {
			feed, err := tx.Bucket(bucketSeen).CreateBucketIfNotExists([]byte(feedURL))
			if err != nil {
				return fmt.Errorf("create bucket: %w", err)
			}
			ts := make([]byte, 8)
			binary.BigEndian.PutUint64(ts, uint64(time.Now().Add(time.Duration(i)*time.Second).Unix()))
			return feed.Put([]byte(guid), ts)
		}))
	}

	require.NoError(t, s.TrimSeen(feedURL, 2))

	seen, err := s.IsSeen(feedURL, "old")
	require.NoError(t, err)
	assert.False(t, seen)
	for _, guid := range []string{"mid", "new"} {
		seen, err := s.IsSeen(feedURL, guid)
		require.NoError(t, err)
		assert.True(t, seen, guid)
	}
}
