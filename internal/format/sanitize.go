package format

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

var (
	// Tags whose entire contents are dropped.
	reDropContent = []*regexp.Regexp{
		regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`),
		regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style\s*>`),
		regexp.MustCompile(`(?is)<iframe\b[^>]*>.*?</iframe\s*>`),
		regexp.MustCompile(`(?is)<noscript\b[^>]*>.*?</noscript\s*>`),
	}

	// HTML comments and doctype declarations
	// (Reddit wraps content in <!-- SC_OFF -->…<!-- SC_ON -->).
	reHTMLCommentDecl = regexp.MustCompile(`(?s)<!--.*?-->|<![^>]*>`)

	// Any HTML tag: opening, closing, or self-closing.
	reHTMLTag = regexp.MustCompile(`<\s*(/?)\s*([a-zA-Z][a-zA-Z0-9-]*)([^>]*)>`)

	// <ol>…</ol> blocks and their <li> tags for numbered-list rendering.
	reOLBlock = regexp.MustCompile(`(?is)<ol[^>]*>(.*?)</ol>`)
	reLi      = regexp.MustCompile(`<li[^>]*>`)

	reBr = regexp.MustCompile(`<br\s*/?\s*>`)

	// Block-level closing tags that map to a newline.
	blockTagReplacer = strings.NewReplacer(
		"</li>", "\n",
		"</p>", "\n",
		"</div>", "\n",
		"</tr>", "\n",
		"</td>", "\n",
	)

	reEmptyAnchor = regexp.MustCompile(`<a [^>]*>\s*</a>`)

	// href attribute within a raw tag.
	reHrefAttr = regexp.MustCompile(`(?i)\bhref\s*=\s*(?:"([^"]*)"|'([^']*)')`)

	// Whitespace entities (Reddit uses &#32; as structural padding).
	reSpaceEntity = regexp.MustCompile(`&(?:nbsp|#0*32|#0*160|#[xX]0*[aA]0);`)

	// Whitespace immediately inside anchor tags — collapses " /u/X " to "/u/X".
	reAnchorPad = regexp.MustCompile(`(<a [^>]*>)\s+|\s+(</a>)`)
)

// Telegram HTML-mode inline tags we keep. Everything else is stripped.
var allowedTags = map[string]bool{
	"a":          true,
	"b":          true,
	"strong":     true,
	"i":          true,
	"em":         true,
	"u":          true,
	"ins":        true,
	"s":          true,
	"strike":     true,
	"del":        true,
	"code":       true,
	"pre":        true,
	"blockquote": true,
	"tg-spoiler": true,
}

// sanitizeHTML converts block tags to newlines and keeps only Telegram-supported inline tags.
func sanitizeHTML(s string) string {
	for _, re := range reDropContent {
		s = re.ReplaceAllString(s, "")
	}
	s = reHTMLCommentDecl.ReplaceAllString(s, "")
	s = numberOL(s)
	s = blockTagReplacer.Replace(s)
	s = reBr.ReplaceAllString(s, "\n")

	s = reHTMLTag.ReplaceAllStringFunc(s, func(match string) string {
		m := reHTMLTag.FindStringSubmatch(match)
		closing := m[1] == "/"
		name := strings.ToLower(m[2])

		if !allowedTags[name] {
			return ""
		}
		if closing {
			return "</" + name + ">"
		}
		if name == "a" {
			if href := extractHref(m[3]); href != "" {
				return `<a href="` + href + `">`
			}
			return ""
		}
		return "<" + name + ">"
	})

	s = reEmptyAnchor.ReplaceAllString(s, "")
	s = reSpaceEntity.ReplaceAllString(s, " ")
	s = reAnchorPad.ReplaceAllString(s, "$1$2")
	return s
}

// extractHref parses an href attribute and returns it re-escaped for HTML output.
func extractHref(attrs string) string {
	m := reHrefAttr.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	href := m[1]
	if href == "" {
		href = m[2]
	}
	if href == "" {
		return ""
	}
	return html.EscapeString(html.UnescapeString(href))
}

// numberOL replaces <li> tags inside <ol> blocks with numbered prefixes.
func numberOL(s string) string {
	return reOLBlock.ReplaceAllStringFunc(s, func(match string) string {
		body := reOLBlock.FindStringSubmatch(match)[1]
		counter := 0
		return reLi.ReplaceAllStringFunc(body, func(string) string {
			counter++
			return strconv.Itoa(counter) + ". "
		})
	})
}

// normalizeLines splits text on newlines, collapses whitespace, and drops empty lines.
func normalizeLines(text string, maxLines int) []string {
	var lines []string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if maxLines > 0 && len(lines) >= maxLines {
			break
		}
	}
	return lines
}
