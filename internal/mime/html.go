package mime

import (
	"strings"

	"golang.org/x/net/html"
)

var blockTags = map[string]bool{
	"br": true, "p": true, "div": true, "tr": true, "li": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
}

// stripHTML extracts a plaintext approximation of an HTML document: text
// content only, with newlines inserted at block-level boundaries and
// <script>/<style> contents dropped entirely. <a href> targets are preserved
// alongside their link text (e.g. "Click here (https://example.com)") so
// links aren't silently lost when the visible text doesn't already show the
// URL — that URL is what lets the TUI render the link as clickable.
func stripHTML(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var sb strings.Builder
	skipDepth := 0 // >0 while inside <script> or <style>

	var linkHref string
	var linkText strings.Builder
	inLink := false

	flushLink := func() {
		text := strings.TrimSpace(linkText.String())
		switch {
		case text == "":
			sb.WriteString(linkHref)
		case strings.EqualFold(text, linkHref):
			sb.WriteString(text)
		default:
			sb.WriteString(text)
			sb.WriteString(" (")
			sb.WriteString(linkHref)
			sb.WriteString(")")
		}
		linkText.Reset()
		linkHref = ""
		inLink = false
	}

	for {
		switch z.Next() {
		case html.ErrorToken:
			if inLink {
				flushLink()
			}
			return collapseBlankLines(sb.String())

		case html.TextToken:
			if skipDepth == 0 {
				if inLink {
					linkText.Write(z.Text())
				} else {
					sb.Write(z.Text())
				}
			}

		case html.StartTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "script", "style":
				skipDepth++
			case "a":
				if inLink {
					flushLink() // malformed nested <a>: flush the outer one first
				}
				if href := tagAttr(z, hasAttr, "href"); href != "" {
					inLink = true
					linkHref = href
				}
			default:
				if blockTags[string(name)] {
					sb.WriteByte('\n')
				}
			}

		case html.SelfClosingTagToken:
			name, _ := z.TagName()
			if blockTags[string(name)] {
				sb.WriteByte('\n')
			}

		case html.EndTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style":
				if skipDepth > 0 {
					skipDepth--
				}
			case "a":
				if inLink {
					flushLink()
				}
			}
		}
	}
}

// tagAttr reads attrKey's value off the tokenizer's current start tag, if
// present. Must be called immediately after TagName() for that tag, before
// any further Next() call advances the tokenizer.
func tagAttr(z *html.Tokenizer, hasAttr bool, attrKey string) string {
	for hasAttr {
		var key, val []byte
		key, val, hasAttr = z.TagAttr()
		if string(key) == attrKey {
			return string(val)
		}
	}
	return ""
}

// collapseBlankLines trims each line and collapses runs of blank lines
// into a single blank line.
func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
