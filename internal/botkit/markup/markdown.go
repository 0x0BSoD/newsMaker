package markup

import (
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
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

// allowedTags is the tag set Telegram accepts in HTML parse mode
// (https://core.telegram.org/bots/api#html-style).
var allowedTags = map[string]bool{
	"b": true, "strong": true,
	"i": true, "em": true,
	"u": true, "ins": true,
	"s": true, "strike": true, "del": true,
	"a":    true,
	"code": true, "pre": true,
	"blockquote": true,
	"tg-spoiler": true,
}

var (
	tagPattern  = regexp.MustCompile(`<\s*(/?)\s*([a-zA-Z][a-zA-Z0-9-]*)([^<>]*)>`)
	hrefPattern = regexp.MustCompile(`(?i)href\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	// codeFence matches LLM output wrapped in a markdown code block.
	codeFence = regexp.MustCompile("(?s)^\\s*```[a-zA-Z]*\\n?(.*?)\\n?```\\s*$")
	// entityPrefix matches an already-encoded HTML entity at the start of a string.
	entityPrefix = regexp.MustCompile(`^&(?:[a-zA-Z]+|#[0-9]+|#x[0-9a-fA-F]+);`)
)

// SanitizeTelegramHTML cleans LLM-generated text so it can be sent as a
// Telegram HTML message without being rejected by the API:
//   - strips a surrounding markdown code fence, if any
//   - converts <br> variants to newlines
//   - keeps only tags Telegram supports (dropping all attributes except a's
//     href), removes everything else (<p>, <ul>, …) while keeping their text
//   - drops stray closing tags and closes tags left open
//   - escapes bare <, > and & in text (already-encoded entities are kept)
func SanitizeTelegramHTML(s string) string {
	if m := codeFence.FindStringSubmatch(s); m != nil {
		s = m[1]
	}
	s = brTag.ReplaceAllString(s, "\n")

	var (
		b     strings.Builder
		stack []string
		last  int
	)

	for _, m := range tagPattern.FindAllStringSubmatchIndex(s, -1) {
		b.WriteString(escapeText(s[last:m[0]]))
		last = m[1]

		closing := m[3] > m[2] // group 1 ("/") is non-empty
		name := strings.ToLower(s[m[4]:m[5]])
		attrs := s[m[6]:m[7]]

		if !allowedTags[name] {
			continue
		}

		if closing {
			// Pop to the matching open tag, closing anything opened inside it;
			// a closer with no matching opener is dropped.
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i] == name {
					for j := len(stack) - 1; j >= i; j-- {
						writeCloseTag(&b, stack[j])
					}
					stack = stack[:i]
					break
				}
			}
			continue
		}

		if strings.HasSuffix(strings.TrimSpace(attrs), "/") {
			continue // self-closing inline tag carries no content
		}

		if name == "a" {
			href := extractHref(attrs)
			if href == "" {
				continue // anchor without href is invalid for Telegram
			}
			b.WriteString(`<a href="`)
			b.WriteString(escapeAttr(href))
			b.WriteString(`">`)
			stack = append(stack, name)
			continue
		}

		b.WriteByte('<')
		b.WriteString(name)
		b.WriteByte('>')
		stack = append(stack, name)
	}

	b.WriteString(escapeText(s[last:]))
	for i := len(stack) - 1; i >= 0; i-- {
		writeCloseTag(&b, stack[i])
	}

	return b.String()
}

func writeCloseTag(b *strings.Builder, name string) {
	b.WriteString("</")
	b.WriteString(name)
	b.WriteByte('>')
}

// TelegramMessageLimit is the maximum message text length the Bot API
// accepts. The split limit is slightly lower to leave headroom for closing
// tags Telegram counts after parsing.
const TelegramMessageLimit = 4000

// SplitMessage splits s into chunks of at most limit runes, breaking at
// newline boundaries so Telegram HTML lines (bullets, headers) stay intact.
// A single line longer than limit is hard-split. Tag pairs spanning multiple
// lines may end up unbalanced across chunks; sanitize each chunk if that
// matters for the parse mode used.
func SplitMessage(s string, limit int) []string {
	if utf8.RuneCountInString(s) <= limit {
		return []string{s}
	}

	var (
		chunks []string
		cur    strings.Builder
		curLen int
	)
	flush := func() {
		if curLen > 0 {
			chunks = append(chunks, strings.TrimRight(cur.String(), "\n"))
			cur.Reset()
			curLen = 0
		}
	}

	for line := range strings.SplitSeq(s, "\n") {
		lineLen := utf8.RuneCountInString(line) + 1 // +1 for the newline

		if lineLen > limit {
			flush()
			r := []rune(line)
			for len(r) > limit {
				chunks = append(chunks, string(r[:limit]))
				r = r[limit:]
			}
			cur.WriteString(string(r))
			cur.WriteByte('\n')
			curLen = len(r) + 1
			continue
		}

		if curLen+lineLen > limit {
			flush()
		}
		cur.WriteString(line)
		cur.WriteByte('\n')
		curLen += lineLen
	}
	flush()

	return chunks
}

// StripHTML removes all HTML tags and decodes entities, for use as a
// plain-text fallback when Telegram rejects the HTML version of a message.
func StripHTML(s string) string {
	s = brTag.ReplaceAllString(s, "\n")
	s = tagPattern.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

func extractHref(attrs string) string {
	m := hrefPattern.FindStringSubmatch(attrs)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return g
		}
	}
	return ""
}

// escapeText escapes <, > and bare & for Telegram HTML; & that already starts
// a valid entity is left as is to avoid double-escaping LLM output.
func escapeText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '&':
			if entityPrefix.MatchString(s[i:]) {
				b.WriteByte('&')
			} else {
				b.WriteString("&amp;")
			}
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func escapeAttr(s string) string {
	return strings.ReplaceAll(escapeText(s), `"`, "&quot;")
}
