package markup

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSanitizeTelegramHTML(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "allowed tags pass through",
			in:   `<b>Topic</b> and <i>news</i>`,
			want: `<b>Topic</b> and <i>news</i>`,
		},
		{
			name: "link with double-quoted href",
			in:   `<a href="https://example.com/x?a=1&b=2">title</a>`,
			want: `<a href="https://example.com/x?a=1&amp;b=2">title</a>`,
		},
		{
			name: "link with single-quoted href is normalized",
			in:   `<a href='https://example.com'>title</a>`,
			want: `<a href="https://example.com">title</a>`,
		},
		{
			name: "anchor without href is dropped",
			in:   `<a>just text</a>`,
			want: `just text`,
		},
		{
			name: "unsupported tags removed, text kept",
			in:   `<p>Hello <ul><li>one</li></ul></p>`,
			want: `Hello one`,
		},
		{
			name: "unclosed tag gets closed",
			in:   `<b>bold til the end`,
			want: `<b>bold til the end</b>`,
		},
		{
			name: "stray closing tag dropped",
			in:   `no bold</b> here`,
			want: `no bold here`,
		},
		{
			name: "br becomes newline",
			in:   `line one<br/>line two`,
			want: "line one\nline two",
		},
		{
			name: "bare angle brackets and ampersand escaped",
			in:   `tokens < 5 & cost > 0`,
			want: `tokens &lt; 5 &amp; cost &gt; 0`,
		},
		{
			name: "existing entities not double-escaped",
			in:   `a &amp; b &lt;tag&gt;`,
			want: `a &amp; b &lt;tag&gt;`,
		},
		{
			name: "markdown code fence stripped",
			in:   "```html\n<b>digest</b>\n```",
			want: `<b>digest</b>`,
		},
		{
			name: "nested unclosed tags closed in order",
			in:   `<b>bold <i>both`,
			want: `<b>bold <i>both</i></b>`,
		},
		{
			name: "closing outer tag closes inner first",
			in:   `<b>bold <i>both</b> plain`,
			want: `<b>bold <i>both</i></b> plain`,
		},
		{
			name: "attributes on allowed tags are dropped",
			in:   `<b class="x">bold</b>`,
			want: `<b>bold</b>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SanitizeTelegramHTML(tt.in))
		})
	}
}

func TestStripHTML(t *testing.T) {
	in := `<b>Good morning!</b><br/>• <a href="https://example.com">Title</a> &amp; more`
	want := "Good morning!\n• Title & more"
	assert.Equal(t, want, StripHTML(in))
}
