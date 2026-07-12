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
		{"trims whitespace inside anchors", `<a href="https://x"> text </a>`, `<a href="https://x">text</a>`},
		{"collapses padded Reddit submitted-by line", `&#32; submitted by &#32; <a href="https://x"> /u/X </a>`, `  submitted by   <a href="https://x">/u/X</a>`},
		{"escapes stray angle brackets", "price 5 < 10 and x > 3", "price 5 &lt; 10 and x &gt; 3"},
		{"escapes bare ampersand", "AT&T sells 5", "AT&amp;T sells 5"},
		{"preserves valid entities", "a &amp; b &lt;c&gt;", "a &amp; b &lt;c&gt;"},
		{"escapes text leaked by a quoted-gt attribute", `<a href="https://x" title="a>b">link</a>`, `<a href="https://x">b&#34;&gt;link</a>`},
		{"drops javascript scheme href but keeps text", `<a href="javascript:alert(1)">x</a>`, "x"},
		{"drops tg scheme href but keeps text", `<a href="tg://resolve?domain=evil">x</a>`, "x"},
		{"drops data scheme href but keeps text", `<a href="data:text/html,evil">x</a>`, "x"},
		{"drops relative href but keeps text", `<a href="/path">x</a>`, "x"},
		{"drops mailto href but keeps text", `<a href="mailto:a@b.com">mail</a>`, "mail"},
		{"drops anchor without href", "<a>x</a>", "x"},
		{"drops empty href anchor", `<a href="">x</a>`, "x"},
		{"drops tag name case-insensitively", "<STRONG>loud</STRONG>", "<strong>loud</strong>"},
		{"ol numbered", "<ol><li>A</li><li>B</li></ol>", "1. A\n2. B\n"},
		{"ul no numbers", "<ul><li>A</li><li>B</li></ul>", "A\nB\n"},
		{"p to newline", "<p>One</p><p>Two</p>", "One\nTwo\n"},
		{"br to newline", "A<br>B<br/>C", "A\nB\nC"},
		{"td to newline", "<table><tr><td>A</td><td>B</td></tr></table>", "A\nB\n\n"},
		{"strips empty anchors", `<a href="x"><img src="y"/></a>text`, "text"},
		{"closes unclosed tag", "<b>bold", "<b>bold</b>"},
		{"closes unclosed anchor", `<a href="https://x">text`, `<a href="https://x">text</a>`},
		{"closes nested unclosed tags", "<b>one <i>two", "<b>one <i>two</i></b>"},
		{"closes interleaved tags at nearest valid point", "<b>one<i>two</b>three</i>", "<b>one<i>two</i></b>three"},
		{"closes innermost of nested same-name tags", "<b>x<b>y</b>z</b>", "<b>x<b>y</b>z</b>"},
		{"drops stray closing tag", "text</b>more", "textmore"},
		{"drops stray anchor closer", "text</a>more", "textmore"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeHTML(tt.in))
		})
	}
}
