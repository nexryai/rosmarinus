package mfm

import (
	"strings"
	"testing"
)

func TestToHTMLRendersSimpleCurrentMFMNodes(t *testing.T) {
	rendered := ToHTML("hello <script>alert('x')</script> https://example.com #fediverse @bob@remote.example :party:", "https://local.example")
	if rendered.Advanced {
		t.Fatal("simple MFM was classified as advanced")
	}
	for _, expected := range []string{
		"hello &lt;script&gt;alert(&#39;x&#39;)&lt;/script&gt;",
		`<a href="https://example.com/">https://example.com</a>`,
		`<a href="https://local.example/tags/fediverse" rel="tag">#fediverse</a>`,
		`<a href="https://local.example/@bob@remote.example" class="u-url mention">@bob@remote.example</a>`,
		"\u200B:party:\u200B",
	} {
		if !strings.Contains(rendered.HTML, expected) {
			t.Errorf("HTML does not contain %q: %s", expected, rendered.HTML)
		}
	}
}

func TestToHTMLMatchesSimpleTokenBoundaries(t *testing.T) {
	rendered := ToHTML("abc#tag abc@bob a:party: #123 @alice. :ok:!", "https://local.example")
	if rendered.Advanced {
		t.Fatal("simple tokens were classified as advanced")
	}
	if strings.Contains(rendered.HTML, "/tags/tag") || strings.Contains(rendered.HTML, "/@bob") || strings.Contains(rendered.HTML, "\u200B:party:") {
		t.Fatalf("token with an alphanumeric prefix was parsed: %s", rendered.HTML)
	}
	if strings.Contains(rendered.HTML, "/tags/123") {
		t.Fatalf("numeric hashtag was parsed: %s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, `href="https://local.example/@alice"`) || !strings.Contains(rendered.HTML, "\u200B:ok:\u200B!") {
		t.Fatalf("valid boundary token was not parsed: %s", rendered.HTML)
	}
}

func TestToHTMLRendersAdvancedMFM(t *testing.T) {
	input := "**bold** ~~gone~~ `code <tag>` \\(x < y\\) $[ruby 漢字 かんじ] $[unixtime 0]"
	rendered := ToHTML(input, "https://local.example")
	if !rendered.Advanced {
		t.Fatal("advanced MFM was classified as simple")
	}
	for _, expected := range []string{
		"<b>bold</b>",
		"<del>gone</del>",
		"<code>code &lt;tag&gt;</code>",
		"<code>x &lt; y</code>",
		"<ruby>漢字<rp>(</rp><rt>かんじ</rt><rp>)</rp></ruby>",
		`<time datetime="1970-01-01T00:00:00.000Z">1970-01-01T00:00:00.000Z</time>`,
	} {
		if !strings.Contains(rendered.HTML, expected) {
			t.Errorf("HTML does not contain %q: %s", expected, rendered.HTML)
		}
	}
}

func TestToHTMLRendersBlockMFM(t *testing.T) {
	input := "> quote\n> **bold**\n\n```go\nif x < y {}\n```\n<center>center</center>"
	rendered := ToHTML(input, "https://local.example")
	if !rendered.Advanced {
		t.Fatal("block MFM was classified as simple")
	}
	for _, expected := range []string{
		"<blockquote>quote<br /><b>bold</b></blockquote>",
		"<pre><code>if x &lt; y {}</code></pre>",
		`<div style="text-align: center;">center</div>`,
	} {
		if !strings.Contains(rendered.HTML, expected) {
			t.Errorf("HTML does not contain %q: %s", expected, rendered.HTML)
		}
	}
}

func TestToHTMLLeavesMalformedAndUnsafeLinksAsText(t *testing.T) {
	rendered := ToHTML("[click](javascript:alert(1)) <img src=x onerror=alert(1)>", "https://local.example")
	if !rendered.Advanced {
		t.Fatal("link syntax must remain an advanced MFM node")
	}
	if strings.Contains(rendered.HTML, "href=\"javascript:") || strings.Contains(rendered.HTML, "<img") {
		t.Fatalf("unsafe HTML escaped incorrectly: %s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, "[click](javascript:alert(1))") || !strings.Contains(rendered.HTML, "&lt;img") {
		t.Fatalf("malformed construct was lost: %s", rendered.HTML)
	}
}

func TestToHTMLStopsParsingAtCurrentMFMNestingLimit(t *testing.T) {
	input := strings.Repeat("**", maxNesting+2) + "text" + strings.Repeat("**", maxNesting+2)
	rendered := ToHTML(input, "https://local.example")
	if !rendered.Advanced {
		t.Fatal("nested bold was classified as simple")
	}
	if strings.Count(rendered.HTML, "<b>") > maxNesting {
		t.Fatalf("nesting limit was not applied: %s", rendered.HTML)
	}
	if !strings.Contains(rendered.HTML, "text") {
		t.Fatalf("nested content was lost: %s", rendered.HTML)
	}
}
