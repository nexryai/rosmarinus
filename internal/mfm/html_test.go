package mfm

import (
	"strings"
	"testing"
)

func TestFromHTMLMatchesCurrentMisskeyFixtures(t *testing.T) {
	tests := map[string]string{
		"<p>a</p><p>b</p>":                                         "a\n\nb",
		"<div>a</div><div>b</div>":                                 "a\nb",
		"<ul><li>a</li><li>b</li></ul>":                            "a\nb",
		"<pre><code>a\nb</code></pre>":                             "```\na\nb\n```",
		"<code>a</code>":                                           "`a`",
		"<blockquote>a\nb</blockquote>":                            "> a\n> b",
		"<p>abc<br><br/>d</p>":                                     "abc\n\nd",
		`<p>a <a href="https://example.com/b">c</a> d</p>`:         "a [c](https://example.com/b) d",
		`<p>a <a href="https://example.com/ä">c</a> d</p>`:         "a [c](<https://example.com/ä>) d",
		`<p>a <a href="https://example.com/b"></a> d</p>`:          "a https://example.com/b d",
		`<p>a <a href="https://example.com/@user">@user</a> d</p>`: "a @user@example.com d",
		"<p><strong>a</strong> <del>b</del> <em>c</em></p>":        "**a** ~~b~~ <i>c</i>",
	}
	for input, expected := range tests {
		t.Run(expected, func(t *testing.T) {
			actual, err := FromHTML(input, nil)
			if err != nil {
				t.Fatalf("FromHTML returned error: %v", err)
			}
			if actual != expected {
				t.Fatalf("FromHTML = %q, want %q", actual, expected)
			}
		})
	}
}

func TestFromHTMLDropsUnsafeLinkTargets(t *testing.T) {
	actual, err := FromHTML(`<p><a href="http://127.0.0.1/secret">click</a> <a href="javascript:alert(1)">x</a> <a href="http://example.com/note">ok</a></p>`, nil)
	if err != nil {
		t.Fatalf("FromHTML returned error: %v", err)
	}
	if strings.Contains(actual, "127.0.0.1") || strings.Contains(actual, "javascript:") {
		t.Fatalf("unsafe href was retained: %q", actual)
	}
	if !strings.Contains(actual, "[ok](http://example.com/note)") {
		t.Fatalf("plain http note link was dropped: %q", actual)
	}
}

func TestFromHTMLConvertsRubyMentionAndHashtag(t *testing.T) {
	input := `<p><ruby>Misskey<rp>(</rp><rt>ミスキー</rt><rp>)</rp></ruby> <a href="https://remote.example/@alice">@alice</a> <a href="https://remote.example/tags/Test">#Test</a></p>`
	actual, err := FromHTML(input, []string{"#test"})
	if err != nil {
		t.Fatalf("FromHTML returned error: %v", err)
	}
	expected := "$[ruby Misskey ミスキー] @alice@remote.example #Test"
	if actual != expected {
		t.Fatalf("FromHTML = %q, want %q", actual, expected)
	}
}
