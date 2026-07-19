package mfm

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
	"golang.org/x/text/unicode/norm"
)

var (
	urlPrefix = regexp.MustCompile(`^https?://[\w/:%#@$&?!()\[\]~.,=+\-]+`)
	urlFull   = regexp.MustCompile(`^https?://[\w/:%#@$&?!()\[\]~.,=+\-]+$`)
)

// FromHTML converts federated HTML into the MFM representation used by the
// current Misskey MfmService.fromHtml implementation.
func FromHTML(input string, hashtagNames []string) (string, error) {
	input = regexp.MustCompile(`(?i)<br\s?/?>\r?\n`).ReplaceAllString(input, "\n")
	root := &html.Node{Type: html.ElementNode, DataAtom: atom.Div, Data: "div"}
	nodes, err := html.ParseFragment(strings.NewReader(input), root)
	if err != nil {
		return "", fmt.Errorf("parse HTML for MFM: %w", err)
	}
	for _, node := range nodes {
		root.AppendChild(node)
	}

	hashtags := make(map[string]struct{}, len(hashtagNames))
	for _, name := range hashtagNames {
		hashtags[normalize(name)] = struct{}{}
	}
	var output strings.Builder
	for node := root.FirstChild; node != nil; node = node.NextSibling {
		analyze(&output, node, hashtags)
	}
	return strings.TrimSpace(output.String()), nil
}

func analyze(output *strings.Builder, node *html.Node, hashtags map[string]struct{}) {
	if node.Type == html.TextNode {
		output.WriteString(node.Data)
		return
	}
	if node.Type != html.ElementNode {
		return
	}

	switch strings.ToUpper(node.Data) {
	case "BR":
		output.WriteByte('\n')
	case "A":
		writeLink(output, node, hashtags)
	case "H1":
		output.WriteString("【")
		analyzeChildren(output, node, hashtags)
		output.WriteString("】\n")
	case "B", "STRONG":
		output.WriteString("**")
		analyzeChildren(output, node, hashtags)
		output.WriteString("**")
	case "SMALL":
		output.WriteString("<small>")
		analyzeChildren(output, node, hashtags)
		output.WriteString("</small>")
	case "S", "DEL":
		output.WriteString("~~")
		analyzeChildren(output, node, hashtags)
		output.WriteString("~~")
	case "I", "EM":
		output.WriteString("<i>")
		analyzeChildren(output, node, hashtags)
		output.WriteString("</i>")
	case "RUBY":
		writeRuby(output, node, hashtags)
	case "PRE":
		if node.FirstChild != nil && node.FirstChild == node.LastChild && node.FirstChild.Type == html.ElementNode && strings.EqualFold(node.FirstChild.Data, "code") {
			output.WriteString("\n```\n")
			output.WriteString(textContent(node.FirstChild))
			output.WriteString("\n```\n")
		} else {
			analyzeChildren(output, node, hashtags)
		}
	case "CODE":
		output.WriteByte('`')
		analyzeChildren(output, node, hashtags)
		output.WriteByte('`')
	case "BLOCKQUOTE":
		if text := textContent(node); text != "" {
			output.WriteString("\n> ")
			output.WriteString(strings.ReplaceAll(text, "\n", "\n> "))
		}
	case "P", "H2", "H3", "H4", "H5", "H6":
		output.WriteString("\n\n")
		analyzeChildren(output, node, hashtags)
	case "DIV", "HEADER", "FOOTER", "ARTICLE", "LI", "DT", "DD":
		output.WriteByte('\n')
		analyzeChildren(output, node, hashtags)
	default:
		analyzeChildren(output, node, hashtags)
	}
}

func analyzeChildren(output *strings.Builder, node *html.Node, hashtags map[string]struct{}) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		analyze(output, child, hashtags)
	}
}

func writeLink(output *strings.Builder, node *html.Node, hashtags map[string]struct{}) {
	text := textContent(node)
	href := attribute(node, "href")
	rel := attribute(node, "rel")
	if _, ok := hashtags[normalize(text)]; ok && href != "" {
		output.WriteString(text)
		return
	}
	if strings.HasPrefix(text, "@") && !strings.HasPrefix(rel, "me ") {
		parts := strings.Split(text, "@")
		if len(parts) == 2 && href != "" {
			if target, err := url.Parse(href); err == nil && target.Hostname() != "" {
				output.WriteString(text + "@" + target.Hostname())
				return
			}
		}
		if len(parts) == 3 {
			output.WriteString(text)
			return
		}
	}
	if href == "" {
		output.WriteString(text)
		return
	}
	if text == "" || text == href {
		if urlFull.MatchString(href) {
			output.WriteString(href)
		} else {
			output.WriteString("<" + href + ">")
		}
		return
	}
	if urlPrefix.MatchString(href) && !urlFull.MatchString(href) {
		output.WriteString("[" + text + "](<" + href + ">)")
	} else {
		output.WriteString("[" + text + "](" + href + ")")
	}
}

func writeRuby(output *strings.Builder, node *html.Node, hashtags map[string]struct{}) {
	type pair struct{ base, ruby string }
	pairs := make([]pair, 0)
	valid := true
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.TextNode && !strings.ContainsAny(child.Data, " \t\r\n[]") {
			pairs = append(pairs, pair{base: child.Data})
			continue
		}
		if child.Type != html.ElementNode {
			continue
		}
		switch strings.ToUpper(child.Data) {
		case "RP":
			continue
		case "RT":
			ruby := textContent(child)
			if len(pairs) == 0 || strings.ContainsAny(ruby, " \t\r\n[]") {
				valid = false
			} else {
				pairs[len(pairs)-1].ruby = ruby
			}
		default:
			valid = false
		}
		if !valid {
			break
		}
	}
	if !valid {
		analyzeChildren(output, node, hashtags)
		return
	}
	for _, pair := range pairs {
		output.WriteString("$[ruby " + pair.base + " " + pair.ruby + "]")
	}
}

func textContent(node *html.Node) string {
	if node.Type == html.TextNode {
		return node.Data
	}
	if node.Type == html.ElementNode && strings.EqualFold(node.Data, "br") {
		return "\n"
	}
	var output strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		output.WriteString(textContent(child))
	}
	return output.String()
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return attr.Val
		}
	}
	return ""
}

func normalize(value string) string {
	return strings.ToLower(norm.NFKC.String(value))
}
