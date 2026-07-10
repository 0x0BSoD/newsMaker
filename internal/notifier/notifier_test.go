package notifier

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

func TestNextScheduledTime(t *testing.T) {
	n := &Notifier{cfg: Config{MorningHour: 9, NoonHour: 12, EveningHour: 18}}

	day := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)
	at := func(hour int) time.Time { return day.Add(time.Duration(hour) * time.Hour) }

	tests := []struct {
		now          time.Time
		wantTime     time.Time
		wantGreeting string
	}{
		{at(8), at(9), "morning"},
		{at(10), at(12), "afternoon"},
		{at(13), at(18), "evening"},
		{at(19), at(9).AddDate(0, 0, 1), "morning"},
	}

	for _, tt := range tests {
		next, greeting := n.nextScheduledTime(tt.now)
		assert.Equal(t, tt.wantTime, next, "now=%v", tt.now)
		assert.Equal(t, tt.wantGreeting, greeting, "now=%v", tt.now)
	}
}

func TestCurrentGreeting(t *testing.T) {
	n := &Notifier{cfg: Config{MorningHour: 9, NoonHour: 12, EveningHour: 18}}

	day := time.Date(2026, 6, 9, 0, 0, 0, 0, time.UTC)

	assert.Equal(t, "morning", n.currentGreeting(day.Add(8*time.Hour)))
	assert.Equal(t, "afternoon", n.currentGreeting(day.Add(13*time.Hour)))
	assert.Equal(t, "evening", n.currentGreeting(day.Add(20*time.Hour)))
}

func TestGroupByTheme(t *testing.T) {
	articles := []model.Article{
		{Title: "a", Categories: []string{"Go", "Backend"}},
		{Title: "b", Categories: []string{"Go"}},
		{Title: "c", Categories: nil},
		{Title: "d", Categories: []string{"  "}},
	}

	grouped := groupByTheme(articles)

	assert.Len(t, grouped["Go"], 2)
	assert.Len(t, grouped["General"], 2)
}

func TestBuildDigestInput(t *testing.T) {
	grouped := map[string][]model.Article{
		"Go":      {{Title: "Go 1.26 released", Link: "https://go.dev", Summary: "short"}},
		"General": {{Title: "Other", Link: "https://example.com"}},
	}

	out := buildDigestInput("morning", grouped, 500)

	assert.Contains(t, out, "Time of day: morning")
	assert.Contains(t, out, "- Go 1.26 released <https://go.dev> — short")
	assert.Contains(t, out, "- Other <https://example.com>\n")
	// Sorted theme order: General before Go.
	assert.Less(t, strings.Index(out, "Topic: General"), strings.Index(out, "Topic: Go"))
}

func TestBuildDigestInput_TruncatesByRunes(t *testing.T) {
	summary := strings.Repeat("é", 15) // 15 runes, 30 bytes
	grouped := map[string][]model.Article{
		"Go": {{Title: "t", Link: "l", Summary: summary}},
	}

	out := buildDigestInput("morning", grouped, 10)

	assert.True(t, utf8.ValidString(out), "truncation must not split a rune")
	assert.Contains(t, out, strings.Repeat("é", 10)+"...")
	assert.NotContains(t, out, strings.Repeat("é", 11))
}

func TestBuildSimpleDigest_EscapesTheme(t *testing.T) {
	grouped := map[string][]model.Article{
		"C&C <tools>": {{Title: "A & B", Link: "https://example.com"}},
	}

	out := buildSimpleDigest("evening", grouped)

	assert.Contains(t, out, "Good evening!")
	assert.Contains(t, out, "<b>C&amp;C &lt;tools&gt;</b>")
	assert.Contains(t, out, `<a href="https://example.com">A &amp; B</a>`)
}
