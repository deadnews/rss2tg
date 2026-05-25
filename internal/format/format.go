// Package format renders gofeed items as Telegram HTML messages.
package format

import (
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

// Link formats an entry as a bold title + URL.
func Link(item *gofeed.Item) string {
	var b strings.Builder
	if item.Title != "" {
		b.WriteString("<b>")
		b.WriteString(html.EscapeString(item.Title))
		b.WriteString("</b>")
		if item.Link != "" {
			b.WriteString("\n")
			b.WriteString(html.EscapeString(item.Link))
		}
		return b.String()
	}
	b.WriteString(html.EscapeString(item.Link))
	return b.String()
}

// Preview formats an entry with a clickable bold title, sanitized content, and feed attribution.
func Preview(item *gofeed.Item, feedTitle, feedLink string) string {
	var b strings.Builder
	writeBoldTitle(&b, itemTitle(item), item.Link)

	if excerpt := extractExcerpt(item, maxExcerptLines); excerpt != "" {
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

	// Prefer full content over summary.
	text := item.Content
	if text == "" {
		text = item.Description
	}
	b.WriteString(strings.Join(normalizeLines(sanitizeHTML(text), 0), "\n"))
	return b.String()
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

func itemTitle(item *gofeed.Item) string {
	if item.Title != "" {
		return item.Title
	}
	return item.Link
}

func extractExcerpt(item *gofeed.Item, maxLines int) string {
	text := item.Description
	if text == "" {
		text = item.Content
	}
	return strings.Join(normalizeLines(sanitizeHTML(text), maxLines), "\n")
}
