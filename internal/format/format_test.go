package format

import (
	"strings"
	"testing"

	"github.com/mmcdole/gofeed"
	"github.com/stretchr/testify/assert"
)

func TestLink(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		item := &gofeed.Item{Title: "Hello World", Link: "https://example.com/post"}
		got := Link(item, "")
		assert.Equal(t, "<b>Hello World</b>\nhttps://example.com/post", got)
	})

	t.Run("escapes HTML in title", func(t *testing.T) {
		item := &gofeed.Item{Title: "A <b>bold</b> & title", Link: "https://example.com"}
		got := Link(item, "")
		assert.Contains(t, got, "&lt;b&gt;bold&lt;/b&gt;")
		assert.Contains(t, got, "&amp; title")
		assert.Contains(t, got, "<b>")
	})

	t.Run("falls back to link when no title", func(t *testing.T) {
		item := &gofeed.Item{Link: "https://example.com/page"}
		got := Link(item, "")
		assert.Equal(t, "https://example.com/page", got)
	})

	t.Run("inserts meta between title and link", func(t *testing.T) {
		item := &gofeed.Item{Title: "Video", Link: "https://youtu.be/abc"}
		got := Link(item, "5:33")
		assert.Equal(t, "<b>Video</b>\n5:33\nhttps://youtu.be/abc", got)
	})
}

func TestPreview(t *testing.T) {
	t.Run("with excerpt and feed title", func(t *testing.T) {
		item := &gofeed.Item{
			Title:       "Post Title",
			Link:        "https://example.com/post",
			Description: "This is a short excerpt.",
		}
		got := Preview(item, "My Feed", "https://example.com/")
		assert.Contains(t, got, `<a href="https://example.com/post"><b>Post Title</b></a>`)
		assert.Contains(t, got, "This is a short excerpt.")
		assert.Contains(t, got, `via <a href="https://example.com/">My Feed</a>`)
	})

	t.Run("bold feed name fallback without feed link", func(t *testing.T) {
		item := &gofeed.Item{
			Title: "Post Title",
			Link:  "https://example.com/post",
		}
		got := Preview(item, "My Feed", "")
		assert.Contains(t, got, `via <b>My Feed</b>`)
	})

	t.Run("Reddit content with preserved links", func(t *testing.T) {
		// Real Reddit atom entry structure.
		item := &gofeed.Item{
			Title:   "This is what Google Maps looked like on launch day in 2005",
			Link:    "https://www.reddit.com/r/MapPorn/comments/1n2dax4/",
			Content: `<table> <tr><td> <a href="https://www.reddit.com/r/MapPorn/comments/1n2dax4/"> <img src="https://preview.redd.it/6j31j4o2qrlf1.jpeg?width=640&amp;crop=smart&amp;auto=webp&amp;s=abc" /> </a> </td><td> &#32; submitted by &#32; <a href="https://www.reddit.com/user/Mackelowsky"> /u/Mackelowsky </a> <br/> <span><a href="https://i.redd.it/6j31j4o2qrlf1.jpeg">[link]</a></span> &#32; <span><a href="https://www.reddit.com/r/MapPorn/comments/1n2dax4/">[comments]</a></span> </td></tr></table>`,
		}
		got := Preview(item, "top scoring links : MapPorn", "https://www.reddit.com/r/MapPorn/top/")
		assert.Contains(t, got, `<a href="https://www.reddit.com/user/Mackelowsky">`)
		assert.Contains(t, got, "/u/Mackelowsky")
		assert.Contains(t, got, `<a href="https://i.redd.it/6j31j4o2qrlf1.jpeg">[link]</a>`)
		assert.Contains(t, got, "[comments]</a>")
		assert.Contains(t, got, "\n")
		assert.Contains(t, got, `via <a href="https://www.reddit.com/r/MapPorn/top/">top scoring links : MapPorn</a>`)
	})

	t.Run("preserves newlines in excerpt", func(t *testing.T) {
		item := &gofeed.Item{
			Title: "Title",
			Link:  "https://example.com",
			Content: `<ol>
<li>First item</li>
<li>Second item</li>
</ol>`,
		}
		got := Preview(item, "", "")
		assert.Contains(t, got, "1. First item\n2. Second item")
	})

	t.Run("limits excerpt lines", func(t *testing.T) {
		lines := make([]string, 10)
		for i := range lines {
			lines[i] = "<p>line</p>"
		}
		item := &gofeed.Item{
			Title:       "Title",
			Link:        "https://example.com",
			Description: strings.Join(lines, ""),
		}
		got := Preview(item, "Feed", "")
		assert.Equal(t, maxExcerptLines, strings.Count(got, "line"))
	})

	t.Run("no feed title", func(t *testing.T) {
		item := &gofeed.Item{
			Title: "Title",
			Link:  "https://example.com",
		}
		got := Preview(item, "", "")
		assert.NotContains(t, got, "via")
	})

	t.Run("uses content when no description", func(t *testing.T) {
		item := &gofeed.Item{
			Title:   "Title",
			Link:    "https://example.com",
			Content: "Content text here.",
		}
		got := Preview(item, "Feed", "")
		assert.Contains(t, got, "Content text here.")
	})
}

func TestText(t *testing.T) {
	t.Run("HN daily numbered list", func(t *testing.T) {
		item := &gofeed.Item{
			Title: "Hacker News Daily Top 30 @2026-04-11",
			Link:  "https://github.com/meixger/hackernews-daily/issues/1304",
			Content: `<ol>
<li><a href="https://www.numerique.gouv.fr/sinformer/espace-presse/souverainete-numerique-reduction-dependances-extra-europeennes/"><strong>France Launches Government Linux Desktop Plan as Windows Exit Begins</strong> <code>www.numerique.gouv.fr</code></a> - <a href="https://news.ycombinator.com/item?id=47716043">423 comments 832 points</a></li>
<li><a href="https://rowan441.github.io/1dchess/chess.html"><strong>1D Chess</strong> <code>rowan441.github.io</code></a> - <a href="https://news.ycombinator.com/item?id=47719740">135 comments 747 points</a></li>
</ol>`,
		}
		got := Text(item)
		lines := strings.Split(got, "\n")
		assert.Contains(t, lines[0], `<b>Hacker News Daily Top 30 @2026-04-11</b></a>`)
		assert.Empty(t, lines[1])
		assert.True(t, strings.HasPrefix(lines[2], "1. "))
		assert.Contains(t, lines[2], `<a href="https://www.numerique.gouv.fr/`)
		assert.Contains(t, lines[2], "France Launches Government Linux Desktop Plan")
		assert.True(t, strings.HasPrefix(lines[3], "2. "))
		assert.Contains(t, lines[3], "1D Chess")
	})

	t.Run("prefers content over description", func(t *testing.T) {
		item := &gofeed.Item{
			Description: "short summary",
			Content:     "full content here",
		}
		got := Text(item)
		assert.Contains(t, got, "full content here")
		assert.NotContains(t, got, "short summary")
	})

	t.Run("falls back to description", func(t *testing.T) {
		item := &gofeed.Item{Description: "only description"}
		got := Text(item)
		assert.Equal(t, "only description", got)
	})
}

func TestQuote(t *testing.T) {
	t.Run("wraps body in expandable blockquote below title", func(t *testing.T) {
		item := &gofeed.Item{
			Title:       "NextBSD",
			Link:        "https://example.com/nextbsd",
			Description: "line one\n\nline two",
		}
		got := Quote(item)
		assert.Contains(t, got, `<b>NextBSD</b></a>`)
		assert.Contains(t, got, "<blockquote expandable>line one\n\nline two</blockquote>")
	})

	t.Run("flattens inner blockquotes to avoid nesting", func(t *testing.T) {
		item := &gofeed.Item{
			Title:   "quoted",
			Content: "before <blockquote>inner quote</blockquote> after",
		}
		got := Quote(item)
		assert.Equal(t, 1, strings.Count(got, "<blockquote"))
		assert.Contains(t, got, "before inner quote after")
	})

	t.Run("omits blockquote when body is empty", func(t *testing.T) {
		item := &gofeed.Item{Title: "no body", Link: "https://example.com"}
		got := Quote(item)
		assert.NotContains(t, got, "<blockquote")
		assert.Contains(t, got, `<b>no body</b></a>`)
	})
}

func TestExtractImage(t *testing.T) {
	t.Run("from item image", func(t *testing.T) {
		item := &gofeed.Item{Image: &gofeed.Image{URL: "https://img.com/1.jpg"}}
		assert.Equal(t, "https://img.com/1.jpg", ExtractImage(item))
	})

	t.Run("from img tag in description", func(t *testing.T) {
		item := &gofeed.Item{Description: `<p><img src="https://img.com/2.jpg"></p>`}
		assert.Equal(t, "https://img.com/2.jpg", ExtractImage(item))
	})

	t.Run("rewrites preview.redd.it to i.redd.it", func(t *testing.T) {
		item := &gofeed.Item{Content: `<img src="https://preview.redd.it/6j31j4o2qrlf1.jpeg?width=640&amp;crop=smart&amp;auto=webp&amp;s=abc">`}
		assert.Equal(t, "https://i.redd.it/6j31j4o2qrlf1.jpeg", ExtractImage(item))
	})

	t.Run("leaves non-preview Reddit URLs untouched", func(t *testing.T) {
		item := &gofeed.Item{Description: `<img src="https://b.thumbs.redditmedia.com/abc.jpg">`}
		assert.Equal(t, "https://b.thumbs.redditmedia.com/abc.jpg", ExtractImage(item))
	})

	t.Run("leaves external-preview.redd.it untouched", func(t *testing.T) {
		item := &gofeed.Item{Description: `<img src="https://external-preview.redd.it/xyz.jpeg?s=sig">`}
		assert.Equal(t, "https://external-preview.redd.it/xyz.jpeg?s=sig", ExtractImage(item))
	})

	t.Run("no image", func(t *testing.T) {
		item := &gofeed.Item{Description: "no images here"}
		assert.Empty(t, ExtractImage(item))
	})
}
