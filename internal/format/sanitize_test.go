package format

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain text", "hello", "hello"},
		{"keeps bold", "<b>Hello</b> world", "<b>Hello</b> world"},
		{"keeps strong", "<strong>Hello</strong>", "<strong>Hello</strong>"},
		{"keeps italic and em", "<i>one</i> <em>two</em>", "<i>one</i> <em>two</em>"},
		{"keeps code and pre", "<code>x</code> <pre>y</pre>", "<code>x</code> <pre>y</pre>"},
		{"keeps links", `Click <a href="https://example.com">here</a>!`, `Click <a href="https://example.com">here</a>!`},
		{"escapes href quotes", `<a href='https://example.com/?q="x"&amp;y=1'>go</a>`, `<a href="https://example.com/?q=&#34;x&#34;&amp;y=1">go</a>`},
		{"strips unknown tag but keeps content", "<span>kept</span>", "kept"},
		{"strips attributes from inline tags", `<b class="x">Hi</b>`, "<b>Hi</b>"},
		{"strips script contents entirely", "before<script>evil()</script>after", "beforeafter"},
		{"strips HTML comments", "before<!-- SC_OFF --><b>x</b><!-- SC_ON -->after", "before<b>x</b>after"},
		{"strips doctype-style declarations", "<!DOCTYPE html>text", "text"},
		{"decodes space entities", "x&#32;y&nbsp;z&#160;w", "x y z w"},
		{"trims whitespace inside anchors", `<a href="u"> text </a>`, `<a href="u">text</a>`},
		{"collapses padded Reddit submitted-by line", `&#32; submitted by &#32; <a href="u"> /u/X </a>`, `  submitted by   <a href="u">/u/X</a>`},
		{"drops tag name case-insensitively", "<STRONG>loud</STRONG>", "<strong>loud</strong>"},
		{"ol numbered", "<ol><li>A</li><li>B</li></ol>", "1. A\n2. B\n"},
		{"ul no numbers", "<ul><li>A</li><li>B</li></ul>", "A\nB\n"},
		{"p to newline", "<p>One</p><p>Two</p>", "One\nTwo\n"},
		{"br to newline", "A<br>B<br/>C", "A\nB\nC"},
		{"td to newline", "<table><tr><td>A</td><td>B</td></tr></table>", "A\nB\n\n"},
		{"strips empty anchors", `<a href="x"><img src="y"/></a>text`, "text"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeHTML(tt.in))
		})
	}
}
