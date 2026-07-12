package format

import (
	"html"
	"regexp"
	"slices"
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

// Telegram HTML-mode inline tags to keep. Everything else is stripped.
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

	s = reSpaceEntity.ReplaceAllString(s, " ")
	s = keepAllowedTags(s)
	s = reEmptyAnchor.ReplaceAllString(s, "")
	s = reAnchorPad.ReplaceAllString(s, "$1$2")
	return s
}

// keepAllowedTags keeps only Telegram-supported tags and escapes all other text.
func keepAllowedTags(s string) string {
	var b strings.Builder
	last := 0
	droppedAnchors := 0
	var open []string
	closeTags := func(downTo int) {
		for _, name := range slices.Backward(open[downTo:]) {
			b.WriteString("</")
			b.WriteString(name)
			b.WriteByte('>')
		}
		open = open[:downTo]
	}
	for _, loc := range reHTMLTag.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(escapeText(s[last:loc[0]]))
		last = loc[1]
		closing := s[loc[2]:loc[3]] == "/"
		name := strings.ToLower(s[loc[4]:loc[5]])
		switch {
		case !allowedTags[name]:
		case name == "a" && closing && droppedAnchors > 0:
			droppedAnchors--
		case closing:
			i := len(open) - 1
			for i >= 0 && open[i] != name {
				i--
			}
			if i >= 0 {
				closeTags(i)
			}
		case name == "a":
			if href := extractHref(s[loc[6]:loc[7]]); href != "" {
				b.WriteString(`<a href="`)
				b.WriteString(href)
				b.WriteString(`">`)
				open = append(open, name)
			} else {
				droppedAnchors++
			}
		default:
			b.WriteByte('<')
			b.WriteString(name)
			b.WriteByte('>')
			open = append(open, name)
		}
	}
	b.WriteString(escapeText(s[last:]))
	closeTags(0)
	return b.String()
}

// escapeText normalizes entities then escapes HTML specials in plain text.
func escapeText(s string) string {
	return html.EscapeString(html.UnescapeString(s))
}

// extractHref returns the href re-escaped for output, or empty if absent or an unsafe scheme.
func extractHref(attrs string) string {
	m := reHrefAttr.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	href := m[1]
	if href == "" {
		href = m[2]
	}
	href = html.UnescapeString(href)
	if !allowedScheme(href) {
		return ""
	}
	return html.EscapeString(href)
}

// allowedScheme reports whether href is safe to render as a Telegram link.
func allowedScheme(href string) bool {
	s := strings.ToLower(strings.TrimSpace(href))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
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

// normalizeText collapses whitespace per line, drops empty lines,
// and keeps at most maxLines lines (0 = no limit).
func normalizeText(text string, maxLines int) string {
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
	return strings.Join(lines, "\n")
}
