package format

import (
	"html"
	"regexp"
	"slices"
	"strconv"
	"strings"

	xhtml "golang.org/x/net/html"
)

var (
	// Anchors left empty once their contents were stripped.
	reEmptyAnchor = regexp.MustCompile(`<a [^>]*>\s*</a>`)

	// Whitespace immediately inside anchor tags — collapses " /u/X " to "/u/X".
	reAnchorPad = regexp.MustCompile(`(<a [^>]*>)\s+|\s+(</a>)`)

	// Runs of blank lines, collapsed to a single paragraph break.
	reNewlineRuns = regexp.MustCompile(`\n{2,}`)
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

// Tags whose entire contents are dropped.
var skippedTags = map[string]bool{
	"script":   true,
	"style":    true,
	"iframe":   true,
	"noscript": true,
}

// Block tags become paragraph breaks.
var blockTags = map[string]bool{
	"p": true, "div": true, "section": true, "article": true, "header": true,
	"footer": true, "aside": true, "nav": true, "main": true, "figure": true,
	"figcaption": true, "ul": true, "ol": true, "dl": true, "table": true,
	"thead": true, "tbody": true, "tfoot": true, "caption": true, "center": true,
	"address": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// Item tags open a line of their own; cell tags separate within that line.
var (
	itemTags = map[string]bool{"li": true, "dt": true, "dd": true, "tr": true}
	cellTags = map[string]bool{"td": true, "th": true}
)

// list is one <ol>/<ul> nesting level, so numbering counts only its own items.
type list struct {
	ordered bool
	n       int
}

// sanitizeHTML maps block tags to paragraph breaks, keeping allowed inline tags.
func sanitizeHTML(in string) string {
	var s sanitizer
	z := xhtml.NewTokenizer(strings.NewReader(in))
	for {
		switch tt := z.Next(); tt {
		case xhtml.ErrorToken:
			return s.finish()
		case xhtml.TextToken:
			s.text(string(z.Text()))
		case xhtml.StartTagToken, xhtml.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			s.startTag(z, string(name), hasAttr, tt == xhtml.StartTagToken)
		case xhtml.EndTagToken:
			name, _ := z.TagName()
			s.endTag(string(name))
		case xhtml.CommentToken, xhtml.DoctypeToken:
		}
	}
}

// sanitizer folds an HTML token stream into Telegram's tag subset. Breaks are
// queued, not written, so they collapse and vanish at a tag's edges.
type sanitizer struct {
	b        strings.Builder
	open     []string // allowed tags awaiting their closing tag
	lists    []list
	skip     int    // depth inside a dropped-content tag
	dropped  int    // anchors dropped but whose text is kept
	pending  string // break held until the next content
	suppress bool   // drop the next break; it would only pad a tag
}

// queueBreak holds the widest break seen; adjacent block tags never stack.
func (s *sanitizer) queueBreak(brk string) {
	if !s.suppress && len(brk) > len(s.pending) {
		s.pending = brk
	}
}

// queueLine adds a line, so consecutive <br> still open a new paragraph.
func (s *sanitizer) queueLine() {
	if !s.suppress && len(s.pending) < 2 {
		s.pending += "\n"
	}
}

func (s *sanitizer) write(str string) {
	s.b.WriteString(s.pending)
	s.b.WriteString(str)
	s.pending = ""
	s.suppress = false
}

// writeOpen emits a tag and drops the break that would pad its first content.
func (s *sanitizer) writeOpen(str string) {
	s.write(str)
	s.suppress = true
}

// writeClose emits a tag, dropping the break that would pad its last content.
func (s *sanitizer) writeClose(str string) {
	s.pending = ""
	s.b.WriteString(str)
	s.suppress = false
}

func (s *sanitizer) closeDownTo(i int) {
	for _, name := range slices.Backward(s.open[i:]) {
		s.writeClose("</" + name + ">")
	}
	s.open = s.open[:i]
}

func (s *sanitizer) text(t string) {
	if s.skip > 0 {
		return
	}
	if slices.Contains(s.open, "pre") {
		// Verbatim, minus the newline HTML drops after an opening tag.
		if s.suppress {
			t = strings.TrimPrefix(t, "\n")
		}
		s.write(escapeText(t))
		return
	}
	if strings.TrimSpace(t) == "" {
		// Beside a queued break or a tag edge, whitespace is markup indentation.
		if s.pending != "" || s.suppress {
			return
		}
		// Between inline content it separates words, however it was written.
		t = " "
	}
	s.write(escapeText(t))
}

func (s *sanitizer) startTag(z *xhtml.Tokenizer, name string, hasAttr, isStart bool) {
	if skippedTags[name] {
		if isStart {
			s.skip++
		}
		return
	}
	if s.skip > 0 {
		return
	}

	switch {
	case name == "br":
		s.queueLine()
	case name == "ol" || name == "ul":
		s.queueBreak("\n\n")
		s.lists = append(s.lists, list{ordered: name == "ol"})
	case itemTags[name]:
		s.queueBreak("\n")
		s.number(name)
	case cellTags[name]:
		s.write(" ")
	case blockTags[name]:
		s.queueBreak("\n\n")
	case !allowedTags[name]:
	case name == "a":
		s.openAnchor(z, hasAttr)
	default:
		s.writeOpen("<" + name + ">")
		s.open = append(s.open, name)
	}
}

// number prefixes a list item when its nearest enclosing list is ordered.
func (s *sanitizer) number(name string) {
	if name != "li" || len(s.lists) == 0 {
		return
	}
	cur := &s.lists[len(s.lists)-1]
	if !cur.ordered {
		return
	}
	cur.n++
	s.write(strconv.Itoa(cur.n) + ". ")
}

func (s *sanitizer) openAnchor(z *xhtml.Tokenizer, hasAttr bool) {
	href := anchorHref(z, hasAttr)
	// Telegram rejects nested links, so an inner anchor keeps only its text.
	if href == "" || slices.Contains(s.open, "a") {
		s.dropped++
		return
	}
	s.writeOpen(`<a href="` + href + `">`)
	s.open = append(s.open, "a")
}

func (s *sanitizer) endTag(name string) {
	if skippedTags[name] {
		if s.skip > 0 {
			s.skip--
		}
		return
	}
	if s.skip > 0 {
		return
	}

	switch {
	case name == "ol" || name == "ul":
		s.queueBreak("\n\n")
		if len(s.lists) > 0 {
			s.lists = s.lists[:len(s.lists)-1]
		}
	case itemTags[name]:
		// Queued now so the next item's indentation reads as markup.
		s.queueBreak("\n")
	case cellTags[name]:
	case blockTags[name]:
		s.queueBreak("\n\n")
	case !allowedTags[name]:
	case name == "a" && s.dropped > 0:
		s.dropped--
	default:
		s.closeInnermost(name)
	}
}

func (s *sanitizer) closeInnermost(name string) {
	for i, open := range slices.Backward(s.open) {
		if open == name {
			s.closeDownTo(i)
			return
		}
	}
}

func (s *sanitizer) finish() string {
	s.closeDownTo(0)

	out := s.b.String()
	out = reEmptyAnchor.ReplaceAllString(out, "")
	out = reAnchorPad.ReplaceAllString(out, "$1$2")

	// Removed tags leave stray blank-line runs; collapse and trim them.
	out = reNewlineRuns.ReplaceAllString(out, "\n\n")
	return strings.Trim(out, "\n")
}

// anchorHref returns the tag's href re-escaped, or empty if absent or unsafe.
func anchorHref(z *xhtml.Tokenizer, hasAttr bool) string {
	for hasAttr {
		var key, val []byte
		key, val, hasAttr = z.TagAttr()
		if string(key) == "href" && allowedScheme(string(val)) {
			return html.EscapeString(string(val))
		}
	}
	return ""
}

// escapeText turns no-break spaces into plain ones and escapes HTML specials.
func escapeText(s string) string {
	return html.EscapeString(strings.ReplaceAll(s, " ", " "))
}

// allowedScheme reports whether href is safe to render as a Telegram link.
func allowedScheme(href string) bool {
	s := strings.ToLower(strings.TrimSpace(href))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// normalizeText collapses whitespace per line outside <pre>, reduces blank-line
// runs to a single paragraph break, and keeps at most maxLines non-blank lines
// (0 = no limit).
func normalizeText(text string, maxLines int) string {
	var lines []string
	content := 0
	verbatim := false
	for line := range strings.SplitSeq(text, "\n") {
		keep := verbatim
		switch {
		case strings.Contains(line, "</pre>"):
			keep, verbatim = true, false
		case strings.Contains(line, "<pre>"):
			keep, verbatim = true, true
		}
		if !keep {
			line = strings.Join(strings.Fields(line), " ")
			if line == "" {
				// Collapse consecutive blanks; skip leading blanks.
				if len(lines) > 0 && lines[len(lines)-1] != "" {
					lines = append(lines, "")
				}
				continue
			}
		}
		lines = append(lines, line)
		content++
		if maxLines > 0 && content >= maxLines {
			break
		}
	}
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return strings.Join(lines, "\n")
}
