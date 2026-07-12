package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTruncateHTML(t *testing.T) {
	t.Run("short text unchanged", func(t *testing.T) {
		assert.Equal(t, "hello", TruncateHTML("hello", 10))
	})

	t.Run("exact limit unchanged", func(t *testing.T) {
		s := strings.Repeat("a", 10)
		assert.Equal(t, s, TruncateHTML(s, 10))
	})

	t.Run("plain text truncated with ellipsis", func(t *testing.T) {
		got := TruncateHTML(strings.Repeat("a", 20), 10)
		assert.Equal(t, strings.Repeat("a", 9)+"…", got)
	})

	t.Run("tags do not count toward limit", func(t *testing.T) {
		s := "<b>" + strings.Repeat("a", 10) + "</b>"
		assert.Equal(t, s, TruncateHTML(s, 10))
	})

	t.Run("closes open tags", func(t *testing.T) {
		got := TruncateHTML("<b>"+strings.Repeat("a", 20)+"</b>", 10)
		assert.Equal(t, "<b>"+strings.Repeat("a", 9)+"…</b>", got)
	})

	t.Run("closes nested tags in order", func(t *testing.T) {
		got := TruncateHTML("<b><i>"+strings.Repeat("a", 20)+"</i></b>", 10)
		assert.Equal(t, "<b><i>"+strings.Repeat("a", 9)+"…</i></b>", got)
	})

	t.Run("closes anchor without attributes", func(t *testing.T) {
		got := TruncateHTML(`<a href="https://e.com">`+strings.Repeat("a", 20)+"</a>", 10)
		assert.Equal(t, `<a href="https://e.com">`+strings.Repeat("a", 9)+"…</a>", got)
	})

	t.Run("entity counts as one visible char", func(t *testing.T) {
		s := strings.Repeat("&amp;", 10)
		assert.Equal(t, s, TruncateHTML(s, 10))
	})

	t.Run("does not split entity", func(t *testing.T) {
		got := TruncateHTML(strings.Repeat("&amp;", 10), 5)
		assert.Equal(t, strings.Repeat("&amp;", 4)+"…", got)
	})

	t.Run("emoji counts two utf16 units", func(t *testing.T) {
		s := strings.Repeat("😀", 5)
		assert.Equal(t, s, TruncateHTML(s, 10))
		assert.Equal(t, strings.Repeat("😀", 4)+"…", TruncateHTML(s, 9))
	})

	t.Run("cut after closing tag leaves it intact", func(t *testing.T) {
		got := TruncateHTML("<b>aaaa</b>"+strings.Repeat("b", 20), 10)
		assert.Equal(t, "<b>aaaa</b>"+strings.Repeat("b", 5)+"…", got)
	})
}

func TestTruncateHTMLRealFeed(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "test", "hn.xml"))
	require.NoError(t, err)
	feed, err := gofeed.NewParser().ParseString(string(data))
	require.NoError(t, err)
	require.NotEmpty(t, feed.Items)

	// The fixture renders well under the Telegram limits; cut tight to
	// force truncation inside real markup.
	const limit = 200
	full := Text(feed.Items[0])
	got := TruncateHTML(full, limit)

	require.NotEqual(t, full, got)
	assert.Contains(t, got, "…")
	// Re-truncating is a no-op: the result is within the limit.
	assert.Equal(t, got, TruncateHTML(got, limit))
	// All tags closed — Telegram rejects unbalanced HTML.
	assert.Equal(t, strings.Count(got, "<a "), strings.Count(got, "</a>"))
	assert.Equal(t, strings.Count(got, "<b>"), strings.Count(got, "</b>"))
}
