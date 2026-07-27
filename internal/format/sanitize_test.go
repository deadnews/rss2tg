package format

import (
	"fmt"
	"maps"
	"slices"
	"strings"
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
		{"keeps gt inside block attribute", `<p title="5 > 3">x</p>y`, "x\n\ny"},
		{"definition list on its own line", "<dl><dt>k</dt><dd>v</dd></dl>", "k\nv"},
		{"table rows on their own line", "<table><tr><td>a</td><td>b</td></tr><tr><td>c</td><td>d</td></tr></table>", " a b\n c d"},
		{"paragraphs still break", "<p>a</p><p>b</p>", "a\n\nb"},
		{"quote drops padding from inner block", "<blockquote><p>x</p></blockquote>", "<blockquote>x</blockquote>"},
		{"quote keeps break between inner blocks", "<blockquote><p>a</p><p>b</p></blockquote>", "<blockquote>a\n\nb</blockquote>"},
		{"newline between inline content spaces words", "<span>a</span>\n<b>b</b>", "a <b>b</b>"},
		{"newline between block content is markup", "<p>a</p>\n<p>b</p>", "a\n\nb"},
		{"nested list numbers only its own level", "<ol><li>A<ul><li>x</li></ul></li><li>B</li></ol>", "1. A\n\nx\n\n2. B"},
		{"keeps gt inside single-quoted attribute", `<b title='a > b'>Hi</b>`, "<b>Hi</b>"},
		{"strips script contents entirely", "before<script>evil()</script>after", "beforeafter"},
		{"strips HTML comments", "before<!-- SC_OFF --><b>x</b><!-- SC_ON -->after", "before<b>x</b>after"},
		{"strips doctype-style declarations", "<!DOCTYPE html>text", "text"},
		{"decodes space entities", "x&#32;y&nbsp;z&#160;w", "x y z w"},
		{"trims whitespace inside anchors", `<a href="https://x"> text </a>`, `<a href="https://x">text</a>`},
		{"collapses padded Reddit submitted-by line", `&#32; submitted by &#32; <a href="https://x"> /u/X </a>`, `  submitted by   <a href="https://x">/u/X</a>`},
		{"escapes stray angle brackets", "price 5 < 10 and x > 3", "price 5 &lt; 10 and x &gt; 3"},
		{"escapes bare ampersand", "AT&T sells 5", "AT&amp;T sells 5"},
		{"preserves valid entities", "a &amp; b &lt;c&gt;", "a &amp; b &lt;c&gt;"},
		{"keeps gt inside inline attribute", `<a href="https://x" title="a>b">link</a>`, `<a href="https://x">link</a>`},
		{"drops nested anchor but keeps its text", `<a href="https://a.com">a <a href="https://b.com">b</a></a>`, `<a href="https://a.com">a b</a>`},
		{"keeps sequential anchors", `<a href="https://a.com">a</a> <a href="https://b.com">b</a>`, `<a href="https://a.com">a</a> <a href="https://b.com">b</a>`},
		{"drops javascript scheme href but keeps text", `<a href="javascript:alert(1)">x</a>`, "x"},
		{"drops tg scheme href but keeps text", `<a href="tg://resolve?domain=evil">x</a>`, "x"},
		{"drops data scheme href but keeps text", `<a href="data:text/html,evil">x</a>`, "x"},
		{"drops relative href but keeps text", `<a href="/path">x</a>`, "x"},
		{"drops mailto href but keeps text", `<a href="mailto:a@b.com">mail</a>`, "mail"},
		{"drops anchor without href", "<a>x</a>", "x"},
		{"drops empty href anchor", `<a href="">x</a>`, "x"},
		{"drops tag name case-insensitively", "<STRONG>loud</STRONG>", "<strong>loud</strong>"},
		{"ol numbered compact", "<ol><li>A</li><li>B</li></ol>", "1. A\n2. B"},
		{"ul no numbers", "<ul><li>A</li><li>B</li></ul>", "A\nB"},
		{"p to paragraph break", "<p>One</p><p>Two</p>", "One\n\nTwo"},
		{"unclosed p to paragraph break", "<p>One<p>Two", "One\n\nTwo"},
		{"br stays single newline", "A<br>B<br/>C", "A\nB\nC"},
		{"td to cell separator", "<table><tr><td>A</td><td>B</td></tr></table>", " A B"},
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

// documents are whole-content samples shaped like real feed payloads, where
// block, inline, entity and malformed markup interact.
var documents = map[string]string{
	"wordpress_article": `<h2>Release 1.4</h2><p>The <b>new</b> build is out. See the <a href="https://e.com/notes">notes</a>.</p>
<figure><img src="https://e.com/a.png" alt="chart"><figcaption>Throughput</figcaption></figure>
<ul><li>Faster startup</li><li>Fewer allocations</li></ul>
<blockquote><p>It just works.</p></blockquote>`,

	"ordered_with_nested": `<ol><li>Install<ul><li>via apt</li><li>via brew</li></ul></li><li>Configure</li><li>Run</li></ol>`,

	"reddit_selfpost": `<!-- SC_OFF --><div class="md"><p>I built a thing.&#32;Source&#32;<a href="https://github.com/x/y">here</a>.</p>
<p>Feedback welcome &mdash; especially on the parser.</p></div><!-- SC_ON -->`,

	"mailing_list": `<p>On Mon, someone wrote:<br>&gt; the patch breaks build<br><br>Fixed in r123.</p>
<pre><code>if (a &lt; b &amp;&amp; c &gt; d) { return; }</code></pre>`,

	"table_content": `<table><thead><tr><th>Name</th><th>Size</th></tr></thead>
<tbody><tr><td>alpha</td><td>1 MB</td></tr><tr><td>beta</td><td>2 MB</td></tr></tbody></table>`,

	"malformed": `<p><b>unclosed bold<p>next para<i>and italic</p><span>stray</span>
<a href="https://e.com" title="a > b">titled</a> &amp; <a href="javascript:x()">bad</a>`,

	"nested_inline": `<p><b>bold <i>and <u>under</u></i></b> then <s>struck</s> and <code>x&lt;y</code>.</p>`,

	"heading_stack": `<h1>Top</h1><h3>Sub</h3><p>Body text.</p><hr><address>me@e.com</address>`,
}

func TestGoldenDocuments(t *testing.T) {
	var b strings.Builder
	for _, name := range slices.Sorted(maps.Keys(documents)) {
		fmt.Fprintf(&b, "=== %s\n%s\n\n", name, sanitizeHTML(documents[name]))
	}
	checkGolden(t, "documents.golden", b.String())
}
