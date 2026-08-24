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
// <script>/<style> contents dropped entirely.
func stripHTML(s string) string {
	z := html.NewTokenizer(strings.NewReader(s))
	var sb strings.Builder
	skipDepth := 0 // >0 while inside <script> or <style>

	for {
		switch z.Next() {
		case html.ErrorToken:
			return collapseBlankLines(sb.String())

		case html.TextToken:
			if skipDepth == 0 {
				sb.Write(z.Text())
			}

		case html.StartTagToken:
			name, _ := z.TagName()
			switch string(name) {
			case "script", "style":
				skipDepth++
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
			if (string(name) == "script" || string(name) == "style") && skipDepth > 0 {
				skipDepth--
			}
		}
	}
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
