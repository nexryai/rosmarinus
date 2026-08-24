package mfm

import (
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const maxNesting = 20

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
)

// Rendered is safe ActivityPub HTML and whether the source used MFM syntax
// that cannot be reconstructed from that HTML without Misskey metadata.
type Rendered struct {
	HTML     string
	Advanced bool
}

// ToHTML renders the federation-facing subset of current MFM.js syntax. Any
// unrecognized or malformed construct remains escaped text.
func ToHTML(input, publicURL string) Rendered {
	r := renderer{publicURL: strings.TrimRight(publicURL, "/")}
	html, advanced := r.render(input, 0)
	return Rendered{HTML: html, Advanced: advanced}
}

func EscapeHTML(value string) string {
	return htmlEscaper.Replace(value)
}

type renderer struct {
	publicURL string
}

func (r renderer) render(input string, depth int) (string, bool) {
	if depth >= maxNesting {
		return renderText(input), false
	}
	var output strings.Builder
	advanced := false
	for i := 0; i < len(input); {
		if isLineStart(input, i) {
			if html, consumed, ok := r.blockCode(input[i:]); ok {
				output.WriteString(html)
				i += consumed
				advanced = true
				continue
			}
			if html, consumed, ok := r.mathBlock(input[i:]); ok {
				output.WriteString(html)
				i += consumed
				advanced = true
				continue
			}
			if html, consumed, ok := r.center(input[i:], depth); ok {
				output.WriteString(html)
				i += consumed
				advanced = true
				continue
			}
			if html, consumed, ok := r.quote(input[i:], depth); ok {
				output.WriteString(html)
				i += consumed
				advanced = true
				continue
			}
			if html, consumed, ok := r.search(input[i:]); ok {
				output.WriteString(html)
				i += consumed
				advanced = true
				continue
			}
		}

		eligible := true
		if i > 0 && (input[i] == '@' || input[i] == '#') && isASCIIAlphaNumeric(input[i-1]) {
			eligible = false
		}
		if i > 0 && input[i] == ':' && isASCIIAlphaNumeric(input[i-1]) {
			eligible = false
		}
		if html, consumed, isAdvanced, ok := r.inline(input[i:], depth); eligible && ok {
			output.WriteString(html)
			i += consumed
			advanced = advanced || isAdvanced
			continue
		}

		end := nextCandidate(input, i+1)
		output.WriteString(renderText(input[i:end]))
		i = end
	}
	return output.String(), advanced
}

func (r renderer) inline(input string, depth int) (string, int, bool, bool) {
	if input == "" {
		return "", 0, false, false
	}
	if strings.HasPrefix(input, "<plain>") {
		if end := strings.Index(input[len("<plain>"):], "</plain>"); end >= 0 {
			content := input[len("<plain>") : len("<plain>")+end]
			return "<span>" + renderText(content) + "</span>", len("<plain>") + end + len("</plain>"), true, true
		}
	}
	for _, tag := range []struct {
		open, close, htmlOpen, htmlClose string
	}{
		{"<small>", "</small>", "<small>", "</small>"},
		{"<b>", "</b>", "<b>", "</b>"},
		{"<i>", "</i>", "<i>", "</i>"},
		{"<s>", "</s>", "<del>", "</del>"},
	} {
		if strings.HasPrefix(input, tag.open) {
			if end := strings.Index(input[len(tag.open):], tag.close); end >= 0 {
				content := input[len(tag.open) : len(tag.open)+end]
				html, _ := r.render(content, depth+1)
				return tag.htmlOpen + html + tag.htmlClose, len(tag.open) + end + len(tag.close), true, true
			}
		}
	}
	if strings.HasPrefix(input, "***") {
		if end := strings.Index(input[3:], "***"); end > 0 {
			content := input[3 : 3+end]
			html, _ := r.render(content, depth+1)
			return "<i>" + html + "</i>", end + 6, true, true
		}
	}
	for _, delimiter := range []string{"**", "__", "~~"} {
		if strings.HasPrefix(input, delimiter) {
			if end := strings.Index(input[len(delimiter):], delimiter); end > 0 {
				content := input[len(delimiter) : len(delimiter)+end]
				if delimiter == "~~" && strings.ContainsAny(content, "~\r\n") {
					continue
				}
				html, _ := r.render(content, depth+1)
				open, close := "<b>", "</b>"
				if delimiter == "~~" {
					open, close = "<del>", "</del>"
				}
				return open + html + close, end + 2*len(delimiter), true, true
			}
		}
	}
	if input[0] == '*' || input[0] == '_' {
		delimiter := input[:1]
		if end := strings.Index(input[1:], delimiter); end > 0 {
			content := input[1 : 1+end]
			if isASCIISpan(content) {
				html, _ := r.render(content, depth+1)
				return "<i>" + html + "</i>", end + 2, true, true
			}
		}
	}
	if input[0] == '`' {
		if end := strings.IndexByte(input[1:], '`'); end > 0 && !strings.ContainsAny(input[1:1+end], "\r\n") {
			return "<code>" + EscapeHTML(input[1:1+end]) + "</code>", end + 2, true, true
		}
	}
	if strings.HasPrefix(input, `\(`) {
		if end := strings.Index(input[2:], `\)`); end > 0 && !strings.ContainsAny(input[2:2+end], "\r\n") {
			return "<code>" + EscapeHTML(input[2:2+end]) + "</code>", end + 4, true, true
		}
	}
	if strings.HasPrefix(input, "?[") || input[0] == '[' {
		prefix := 1
		if strings.HasPrefix(input, "?[") {
			prefix = 2
		}
		if labelEnd := strings.Index(input[prefix:], "]("); labelEnd > 0 {
			labelEnd += prefix
			if urlEnd := strings.IndexByte(input[labelEnd+2:], ')'); urlEnd >= 0 {
				rawURL := input[labelEnd+2 : labelEnd+2+urlEnd]
				label, _ := r.render(input[prefix:labelEnd], depth+1)
				consumed := labelEnd + 3 + urlEnd
				if href, ok := safeURL(rawURL); ok {
					return `<a href="` + EscapeHTML(href) + `">` + label + `</a>`, consumed, true, true
				}
				return "[" + label + "](" + EscapeHTML(rawURL) + ")", consumed, true, true
			}
		}
	}
	if strings.HasPrefix(input, "$[") {
		if end := matchingFunctionEnd(input); end > 0 {
			raw := input[2:end]
			space := strings.IndexAny(raw, " \t\r\n")
			if space > 0 && strings.TrimSpace(raw[space:]) != "" {
				header := raw[:space]
				name := strings.ToLower(strings.SplitN(header, ".", 2)[0])
				content := strings.TrimLeft(raw[space:], " \t\r\n")
				html := r.renderFunction(name, content, depth)
				return html, end + 1, true, true
			}
		}
	}
	if strings.HasPrefix(input, "<http://") || strings.HasPrefix(input, "<https://") {
		if end := strings.IndexByte(input, '>'); end > 1 && !strings.ContainsAny(input[1:end], " \t\r\n") {
			if href, ok := safeURL(input[1:end]); ok {
				return `<a href="` + EscapeHTML(href) + `">` + EscapeHTML(input[1:end]) + `</a>`, end + 1, false, true
			}
		}
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		end := bareURLEnd(input)
		if end > 0 {
			if href, ok := safeURL(input[:end]); ok {
				return `<a href="` + EscapeHTML(href) + `">` + EscapeHTML(input[:end]) + `</a>`, end, false, true
			}
		}
	}
	if input[0] == '@' {
		if end := mentionEnd(input); end > 1 {
			acct := input[:end]
			href := r.publicURL + "/" + acct
			if normalized, ok := safeURL(href); ok {
				return `<a href="` + EscapeHTML(normalized) + `" class="u-url mention">` + EscapeHTML(acct) + `</a>`, end, false, true
			}
			return EscapeHTML(acct), end, false, true
		}
	}
	if input[0] == '#' {
		if end := hashtagEnd(input); end > 1 {
			tag := input[1:end]
			if !onlyDigits(tag) {
				href := r.publicURL + "/tags/" + url.PathEscape(tag)
				return `<a href="` + EscapeHTML(href) + `" rel="tag">#` + EscapeHTML(tag) + `</a>`, end, false, true
			}
		}
	}
	if input[0] == ':' {
		if end := strings.IndexByte(input[1:], ':'); end > 0 {
			end++
			name := input[1:end]
			if isEmojiName(name) && (end+1 == len(input) || !isASCIIAlphaNumeric(input[end+1])) {
				return "\u200B:" + EscapeHTML(name) + ":\u200B", end + 1, false, true
			}
		}
	}
	return "", 0, false, false
}

func (r renderer) renderFunction(name, content string, depth int) string {
	switch name {
	case "unixtime":
		value := strings.Fields(content)
		if len(value) > 0 {
			if seconds, err := strconv.ParseInt(value[0], 10, 64); err == nil {
				formatted := time.Unix(seconds, 0).UTC().Format("2006-01-02T15:04:05.000Z")
				return `<time datetime="` + formatted + `">` + formatted + `</time>`
			}
		}
	case "ruby":
		parts := strings.Fields(content)
		if len(parts) >= 2 {
			reading := parts[len(parts)-1]
			baseEnd := strings.LastIndex(content, reading)
			base := strings.TrimSpace(content[:baseEnd])
			baseHTML, _ := r.render(base, depth+1)
			return "<ruby>" + baseHTML + "<rp>(</rp><rt>" + EscapeHTML(reading) + "</rt><rp>)</rp></ruby>"
		}
	}
	html, _ := r.render(content, depth+1)
	return "<i>" + html + "</i>"
}

func (renderer) blockCode(input string) (string, int, bool) {
	if !strings.HasPrefix(input, "```") {
		return "", 0, false
	}
	firstLineEnd := strings.IndexByte(input, '\n')
	if firstLineEnd < 0 {
		return "", 0, false
	}
	contentStart := firstLineEnd + 1
	for offset := contentStart; offset < len(input); {
		lineEnd := strings.IndexByte(input[offset:], '\n')
		if lineEnd < 0 {
			lineEnd = len(input) - offset
		}
		line := strings.TrimSuffix(input[offset:offset+lineEnd], "\r")
		if strings.HasPrefix(line, "```") {
			code := strings.TrimSuffix(input[contentStart:offset], "\n")
			code = strings.TrimSuffix(code, "\r")
			return "<pre><code>" + EscapeHTML(code) + "</code></pre>", offset + lineEnd, true
		}
		offset += lineEnd
		if offset < len(input) {
			offset++
		}
	}
	return "", 0, false
}

func (renderer) mathBlock(input string) (string, int, bool) {
	if !strings.HasPrefix(input, `\[`) {
		return "", 0, false
	}
	if end := strings.Index(input[2:], `\]`); end >= 0 {
		end += 2
		after := end + 2
		if after == len(input) || input[after] == '\n' || (input[after] == '\r' && after+1 < len(input) && input[after+1] == '\n') {
			formula := strings.TrimSpace(input[2:end])
			return "<pre><code>" + EscapeHTML(formula) + "</code></pre>", after, true
		}
	}
	return "", 0, false
}

func (r renderer) center(input string, depth int) (string, int, bool) {
	if !strings.HasPrefix(input, "<center>") {
		return "", 0, false
	}
	if end := strings.Index(input[len("<center>"):], "</center>"); end >= 0 {
		content := strings.TrimSpace(input[len("<center>") : len("<center>")+end])
		html, _ := r.render(content, depth+1)
		return `<div style="text-align: center;">` + html + "</div>", len("<center>") + end + len("</center>"), true
	}
	return "", 0, false
}

func (r renderer) quote(input string, depth int) (string, int, bool) {
	if input[0] != '>' {
		return "", 0, false
	}
	var content strings.Builder
	consumed := 0
	for consumed < len(input) {
		lineEnd := strings.IndexByte(input[consumed:], '\n')
		if lineEnd < 0 {
			lineEnd = len(input) - consumed
		}
		line := strings.TrimSuffix(input[consumed:consumed+lineEnd], "\r")
		if !strings.HasPrefix(line, ">") {
			break
		}
		line = strings.TrimPrefix(line, ">")
		line = strings.TrimPrefix(line, " ")
		if content.Len() > 0 {
			content.WriteByte('\n')
		}
		content.WriteString(line)
		consumed += lineEnd
		if consumed < len(input) {
			consumed++
		} else {
			break
		}
	}
	if consumed == 0 {
		return "", 0, false
	}
	html, _ := r.render(content.String(), depth+1)
	return "<blockquote>" + html + "</blockquote>", consumed, true
}

func (renderer) search(input string) (string, int, bool) {
	lineEnd := strings.IndexByte(input, '\n')
	if lineEnd < 0 {
		lineEnd = len(input)
	}
	line := strings.TrimSuffix(input[:lineEnd], "\r")
	for _, suffix := range []string{" [Search]", " [検索]", " Search", " 検索"} {
		if len(line) > len(suffix) && strings.EqualFold(line[len(line)-len(suffix):], suffix) {
			query := line[:len(line)-len(suffix)]
			href := "https://www.google.com/search?q=" + strings.ReplaceAll(url.QueryEscape(query), "+", "%20")
			return `<a href="` + EscapeHTML(href) + `">` + EscapeHTML(line) + `</a>`, lineEnd, true
		}
	}
	return "", 0, false
}

func safeURL(raw string) (string, bool) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", false
	}
	if parsed.Path == "" {
		parsed.Path = "/"
	}
	return parsed.String(), true
}

func renderText(value string) string {
	escaped := EscapeHTML(value)
	escaped = strings.ReplaceAll(escaped, "\r\n", "\n")
	escaped = strings.ReplaceAll(escaped, "\r", "\n")
	return strings.ReplaceAll(escaped, "\n", "<br />")
}

func isLineStart(input string, index int) bool {
	return index == 0 || input[index-1] == '\n'
}

func nextCandidate(input string, start int) int {
	for i := start; i < len(input); i++ {
		switch input[i] {
		case '<', '>', '\\', '`', '*', '_', '~', '?', '[', '$', '@', '#', ':', 'h', '\n':
			return i
		}
	}
	return len(input)
}

func matchingFunctionEnd(input string) int {
	level := 1
	for i := 2; i < len(input); i++ {
		switch input[i] {
		case '[':
			level++
		case ']':
			level--
			if level == 0 {
				return i
			}
		}
	}
	return -1
}

func bareURLEnd(input string) int {
	end := 0
	paren, bracket := 0, 0
	for end < len(input) {
		c := input[end]
		if c >= 0x80 || strings.ContainsRune(" \t\r\n<>\"'", rune(c)) {
			break
		}
		switch c {
		case '(':
			paren++
		case ')':
			if paren == 0 {
				goto done
			}
			paren--
		case '[':
			bracket++
		case ']':
			if bracket == 0 {
				goto done
			}
			bracket--
		}
		end++
	}
done:
	for end > 0 && (input[end-1] == '.' || input[end-1] == ',') {
		end--
	}
	return end
}

func mentionEnd(input string) int {
	if len(input) < 2 || input[1] == '-' || input[1] == '.' {
		return 0
	}
	usernameEnd := 1
	for usernameEnd < len(input) && isAccountChar(input[usernameEnd]) {
		usernameEnd++
	}
	if usernameEnd < len(input) && input[usernameEnd] == '@' {
		if input[usernameEnd-1] == '-' || input[usernameEnd-1] == '.' {
			return 0
		}
		hostStart := usernameEnd + 1
		if hostStart >= len(input) || input[hostStart] == '-' || input[hostStart] == '.' {
			return 0
		}
		hostEnd := hostStart
		for hostEnd < len(input) && isAccountChar(input[hostEnd]) {
			hostEnd++
		}
		for hostEnd > hostStart && (input[hostEnd-1] == '-' || input[hostEnd-1] == '.') {
			hostEnd--
		}
		if hostEnd == hostStart {
			return 0
		}
		return hostEnd
	}
	for usernameEnd > 1 && (input[usernameEnd-1] == '-' || input[usernameEnd-1] == '.') {
		usernameEnd--
	}
	if usernameEnd == 1 {
		return 0
	}
	return usernameEnd
}

func isAccountChar(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_' || value == '-' || value == '.'
}

func isASCIIAlphaNumeric(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func hashtagEnd(input string) int {
	i := 1
	for i < len(input) {
		r, size := utf8Rune(input[i:])
		if unicode.IsSpace(r) || strings.ContainsRune(".,!?'\"#:/<>【】()「」（）[]", r) {
			break
		}
		i += size
	}
	return i
}

func utf8Rune(input string) (rune, int) {
	for _, r := range input {
		return r, len(string(r))
	}
	return 0, 0
}

func onlyDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isEmojiName(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '+' || r == '-') {
			return false
		}
	}
	return true
}

func isASCIISpan(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == ' ' || r == '\t') {
			return false
		}
	}
	return true
}
