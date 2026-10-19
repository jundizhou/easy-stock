package themelibrary

import (
	"strings"

	"golang.org/x/net/html"
)

// SanitizeHTML converts the upstream rich-text introduction into plain text.
//
// BigAMap returns legacy editor HTML (inline fonts, images, tables). The
// backend keeps the information but never forwards the markup: the frontend
// renders paragraphs, which also removes the script/iframe surface entirely.
func SanitizeHTML(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "<") {
		return normalizeText(trimmed)
	}
	doc, err := html.Parse(strings.NewReader(trimmed))
	if err != nil {
		return normalizeText(trimmed)
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "script", "style", "noscript", "iframe", "svg":
				return
			case "br":
				appendBreak(&builder, 1)
				return
			case "img":
				return
			case "p", "div", "section", "article", "table", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6":
				appendBreak(&builder, 2)
			case "li":
				appendBreak(&builder, 1)
				if !strings.HasSuffix(builder.String(), "· ") {
					builder.WriteString("· ")
				}
			case "tr":
				appendBreak(&builder, 1)
			case "td", "th":
				if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") && !strings.HasSuffix(builder.String(), " ") {
					builder.WriteString(" ")
				}
			}
		}
		if node.Type == html.TextNode {
			builder.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if node.Type == html.ElementNode {
			switch node.Data {
			case "p", "div", "section", "article", "table", "ul", "ol", "h1", "h2", "h3", "h4", "h5", "h6", "li", "tr":
				appendBreak(&builder, 2)
			case "td", "th":
				builder.WriteString(" ")
			}
		}
	}
	walk(doc)
	return normalizeText(builder.String())
}

func appendBreak(builder *strings.Builder, count int) {
	if builder.Len() == 0 {
		return
	}
	value := builder.String()
	existing := 0
	for i := len(value) - 1; i >= 0 && value[i] == '\n'; i-- {
		existing++
	}
	for existing < count {
		builder.WriteByte('\n')
		existing++
	}
}

func normalizeText(input string) string {
	input = strings.ReplaceAll(input, "\r\n", "\n")
	input = strings.ReplaceAll(input, "\r", "\n")
	input = strings.ReplaceAll(input, "\u00a0", " ")
	input = strings.ReplaceAll(input, "\u200b", "")
	// A handful of upstream intros use <TEXT> (escaped in the source HTML) as a
	// placeholder for an image/chart that is not part of the API payload.
	input = strings.ReplaceAll(input, "<TEXT>", "")
	input = strings.ReplaceAll(input, "<text>", "")
	lines := strings.Split(input, "\n")
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			continue
		}
		normalized = append(normalized, line)
	}
	return strings.Join(normalized, "\n\n")
}
