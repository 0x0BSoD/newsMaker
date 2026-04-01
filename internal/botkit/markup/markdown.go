package markup

import (
	"regexp"
	"strings"
)

// boldPattern matches **text** (standard markdown bold).
var boldPattern = regexp.MustCompile(`\*\*(.+?)\*\*`)

// ConvertToMDV2 converts a string that may contain standard markdown formatting
// into a properly escaped Telegram MarkdownV2 string.
// Supported conversions:
//   - **text** → *text* (bold)
//   - Everything else is escaped per Telegram MarkdownV2 rules.
func ConvertToMDV2(src string) string {
	matches := boldPattern.FindAllStringSubmatchIndex(src, -1)
	if len(matches) == 0 {
		return EscapeForMarkdown(src)
	}

	var b strings.Builder
	last := 0
	for _, m := range matches {
		// escape text before this match
		b.WriteString(EscapeForMarkdown(src[last:m[0]]))
		// wrap inner content as MarkdownV2 bold
		b.WriteByte('*')
		b.WriteString(EscapeForMarkdown(src[m[2]:m[3]]))
		b.WriteByte('*')
		last = m[1]
	}
	b.WriteString(EscapeForMarkdown(src[last:]))
	return b.String()
}

var (
	replacer = strings.NewReplacer(
		"-",
		"\\-",
		"_",
		"\\_",
		"*",
		"\\*",
		"[",
		"\\[",
		"]",
		"\\]",
		"(",
		"\\(",
		")",
		"\\)",
		"~",
		"\\~",
		"`",
		"\\`",
		">",
		"\\>",
		"#",
		"\\#",
		"+",
		"\\+",
		"=",
		"\\=",
		"|",
		"\\|",
		"{",
		"\\{",
		"}",
		"\\}",
		".",
		"\\.",
		"!",
		"\\!",
	)
)

func EscapeForMarkdown(src string) string {
	return replacer.Replace(src)
}

var htmlReplacer = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

func EscapeForHTML(src string) string {
	return htmlReplacer.Replace(src)
}

// brTag matches <br>, <br/>, <br /> in any case.
var brTag = regexp.MustCompile(`(?i)<br\s*/?>`)

// anyTag matches any HTML-like tag.
var anyTag = regexp.MustCompile(`<[^>]+>`)

// SanitizeTelegramHTML cleans LLM-generated text before embedding it into a
// Telegram HTML message. It converts <br> to newlines and strips all other
// HTML tags (the LLM should not produce Telegram HTML; the caller adds its own
// structural tags around the summary).
func SanitizeTelegramHTML(s string) string {
	s = brTag.ReplaceAllString(s, "\n")
	s = anyTag.ReplaceAllString(s, "")
	return s
}
