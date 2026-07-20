// Package format renders gofeed items as Telegram HTML messages.
package format

import (
	"cmp"
	"html"
	"regexp"
	"strings"

	"github.com/mmcdole/gofeed"
)

const maxExcerptLines = 5

var (
	reImgSrc = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

	// preview.redd.it serves signed WebP Telegram can't fetch;
	// i.redd.it serves the same media unsigned.
	reRedditPreviewImg = regexp.MustCompile(`^https?://preview\.redd\.it/([^?]+)`)
)

// Link formats an entry as a bold title + URL;
// meta inserts between them when non-empty.
func Link(item *gofeed.Item, meta string) string {
	if item.Title == "" {
		return html.EscapeString(item.Link)
	}
	var b strings.Builder
	b.WriteString("<b>")
	b.WriteString(html.EscapeString(item.Title))
	b.WriteString("</b>")
	if meta != "" {
		b.WriteString("\n")
		b.WriteString(meta)
	}
	if item.Link != "" {
		b.WriteString("\n")
		b.WriteString(html.EscapeString(item.Link))
	}
	return b.String()
}

// Preview formats an entry with a clickable bold title, sanitized content, and feed attribution.
func Preview(item *gofeed.Item, feedTitle, feedLink string) string {
	var b strings.Builder
	writeBoldTitle(&b, cmp.Or(item.Title, item.Link), item.Link)

	if excerpt := extractExcerpt(item); excerpt != "" {
		b.WriteString("\n\n")
		b.WriteString(excerpt)
	}

	if feedTitle != "" {
		b.WriteString("\n\nvia ")
		if feedLink != "" {
			b.WriteString(`<a href="`)
			b.WriteString(html.EscapeString(feedLink))
			b.WriteString(`">`)
			b.WriteString(html.EscapeString(feedTitle))
			b.WriteString("</a>")
		} else {
			b.WriteString("<b>")
			b.WriteString(html.EscapeString(feedTitle))
			b.WriteString("</b>")
		}
	}

	return b.String()
}

// Text formats an entry with title and sanitized content.
func Text(item *gofeed.Item) string {
	var b strings.Builder

	if item.Title != "" {
		writeBoldTitle(&b, item.Title, item.Link)
		b.WriteString("\n\n")
	}

	b.WriteString(entryBody(item))
	return b.String()
}

// Quote formats an entry with title and its content in an expandable blockquote.
func Quote(item *gofeed.Item) string {
	var b strings.Builder

	if item.Title != "" {
		writeBoldTitle(&b, item.Title, item.Link)
	}

	if body := stripBlockquotes(entryBody(item)); body != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("<blockquote expandable>")
		b.WriteString(body)
		b.WriteString("</blockquote>")
	}
	return b.String()
}

// entryBody renders the entry's full content, preferring content over summary.
func entryBody(item *gofeed.Item) string {
	text := cmp.Or(item.Content, item.Description)
	return normalizeText(sanitizeHTML(text), 0)
}

// stripBlockquotes removes blockquote tags so wrapping the body in
// an outer expandable blockquote can't nest, which Telegram rejects.
func stripBlockquotes(s string) string {
	s = strings.ReplaceAll(s, "<blockquote>", "")
	s = strings.ReplaceAll(s, "</blockquote>", "")
	return s
}

// writeBoldTitle writes `<a href="LINK"><b>TITLE</b></a>`, or `<b>TITLE</b>` if link is empty.
func writeBoldTitle(b *strings.Builder, title, link string) {
	if link != "" {
		b.WriteString(`<a href="`)
		b.WriteString(html.EscapeString(link))
		b.WriteString(`"><b>`)
		b.WriteString(html.EscapeString(title))
		b.WriteString("</b></a>")
		return
	}
	b.WriteString("<b>")
	b.WriteString(html.EscapeString(title))
	b.WriteString("</b>")
}

// ExtractImage returns the first image URL from the feed item, or empty string.
func ExtractImage(item *gofeed.Item) string {
	if item.Image != nil && item.Image.URL != "" {
		return redditDirectURL(item.Image.URL)
	}
	for _, src := range []string{item.Description, item.Content} {
		if m := reImgSrc.FindStringSubmatch(src); m != nil {
			return redditDirectURL(html.UnescapeString(m[1]))
		}
	}
	return ""
}

// redditDirectURL rewrites preview.redd.it → i.redd.it so Telegram can fetch the image.
func redditDirectURL(u string) string {
	if m := reRedditPreviewImg.FindStringSubmatch(u); m != nil {
		return "https://i.redd.it/" + m[1]
	}
	return u
}

func extractExcerpt(item *gofeed.Item) string {
	text := cmp.Or(item.Description, item.Content)
	return normalizeText(sanitizeHTML(text), maxExcerptLines)
}
