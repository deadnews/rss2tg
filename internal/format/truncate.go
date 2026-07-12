package format

import (
	"html"
	"slices"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// Telegram Bot API length limits, in visible UTF-16 code units.
const (
	MessageLimit = 4096
	CaptionLimit = 1024
)

// TruncateHTML truncates Telegram HTML to limit visible UTF-16 code units,
// appending an ellipsis and closing open tags.
func TruncateHTML(s string, limit int) string {
	count := 0
	cut := -1
	var open, cutOpen []string
	for i := 0; i < len(s); {
		if s[i] == '<' {
			end := strings.IndexByte(s[i:], '>')
			if end < 0 {
				break
			}
			open = applyTag(open, s[i+1:i+end])
			i += end + 1
			continue
		}
		units, size := entityLen(s[i:])
		if size == 0 {
			r, n := utf8.DecodeRuneInString(s[i:])
			units, size = utf16.RuneLen(r), n
		}
		// Reserve one unit for the ellipsis.
		if cut < 0 && count+units > limit-1 {
			cut = i
			cutOpen = slices.Clone(open)
		}
		count += units
		i += size
	}
	if count <= limit || cut < 0 {
		return s
	}

	var b strings.Builder
	b.WriteString(s[:cut])
	b.WriteString("…")
	for _, name := range slices.Backward(cutOpen) {
		b.WriteString("</")
		b.WriteString(name)
		b.WriteString(">")
	}
	return b.String()
}

// applyTag pushes an opening tag name onto the stack or pops on a closing tag.
func applyTag(open []string, tag string) []string {
	if strings.HasPrefix(tag, "/") {
		if len(open) > 0 {
			return open[:len(open)-1]
		}
		return open
	}
	name, _, _ := strings.Cut(tag, " ")
	return append(open, name)
}

// entityLen returns the visible UTF-16 length and byte size of a leading
// HTML entity, or zeros if there is none.
func entityLen(s string) (units, size int) {
	if s[0] != '&' {
		return 0, 0
	}
	semi := strings.IndexByte(s, ';')
	if semi < 1 || semi > 32 {
		return 0, 0
	}
	ent := s[:semi+1]
	decoded := html.UnescapeString(ent)
	if decoded == ent {
		return 0, 0
	}
	for _, r := range decoded {
		units += utf16.RuneLen(r)
	}
	return units, len(ent)
}
