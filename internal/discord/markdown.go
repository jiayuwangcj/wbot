package discord

import (
	"html"
	"strings"
)

// ToMarkdown converts the wheel Telegram HTML alert layout to Discord
// Markdown: <b>/<strong> → **, <code> → `, <br> → newline; remaining tags are
// stripped and HTML entities unescaped (the alert text is internal, no user
// HTML). The box-drawing rules and emoji pass through unchanged.
func ToMarkdown(s string) string {
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<b>", "**")
	s = strings.ReplaceAll(s, "</b>", "**")
	s = strings.ReplaceAll(s, "<strong>", "**")
	s = strings.ReplaceAll(s, "</strong>", "**")
	s = strings.ReplaceAll(s, "<code>", "`")
	s = strings.ReplaceAll(s, "</code>", "`")
	s = strings.ReplaceAll(s, "<i>", "*")
	s = strings.ReplaceAll(s, "</i>", "*")
	s = stripTags(s)
	return html.UnescapeString(s)
}

func stripTags(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return b.String()
}
