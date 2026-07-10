package notifier

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit/markup"
	"github.com/0x0BSoD/newsMaker/internal/model"
	"github.com/0x0BSoD/newsMaker/internal/reporter"
)

type ArticleProvider interface {
	AllNotPosted(ctx context.Context, since time.Time, limit uint64) ([]model.Article, error)
	MarkAsPosted(ctx context.Context, articleIDs []int64) error
}

type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
	CountTokens(text string) (int, error)
}

type Notifier struct {
	articles   ArticleProvider
	summarizer Summarizer
	bot        *tgbotapi.BotAPI
	reporter   *reporter.Reporter
	cfg        Config
}

// Config holds the scheduling and digest-building parameters for a Notifier.
type Config struct {
	ChannelID       int64
	MorningHour     int
	NoonHour        int
	EveningHour     int
	Lookback        time.Duration
	MaxArticles     int
	RetryInterval   time.Duration
	MaxRetries      int
	SummaryInputDir string
	MaxInputDataLen int
}

func New(
	articleProvider ArticleProvider,
	summarizer Summarizer,
	bot *tgbotapi.BotAPI,
	rep *reporter.Reporter,
	cfg Config,
) *Notifier {
	return &Notifier{
		articles:   articleProvider,
		summarizer: summarizer,
		bot:        bot,
		reporter:   rep,
		cfg:        cfg,
	}
}

func (n *Notifier) Start(ctx context.Context) error {
	slog.Info("notifier started (digest mode)")

	for {
		next, greeting := n.nextScheduledTime(time.Now())
		slog.Info("next digest scheduled", "at", next, "slot", greeting)

		select {
		case <-time.After(time.Until(next)):
			n.sendWithRetry(ctx, greeting)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// sendWithRetry attempts SendDigest up to maxRetries times, waiting
// retryInterval between attempts. Stops early if ctx is cancelled.
func (n *Notifier) sendWithRetry(ctx context.Context, greeting string) {
	for attempt := 1; attempt <= n.cfg.MaxRetries; attempt++ {
		err := n.SendDigest(ctx, greeting)
		if err == nil {
			return
		}

		slog.Error("digest send failed", "err", err, "attempt", attempt, "maxRetries", n.cfg.MaxRetries)
		n.reporter.Notify(fmt.Sprintf("Digest error (attempt %d/%d): %v", attempt, n.cfg.MaxRetries, err))

		if attempt == n.cfg.MaxRetries {
			slog.Error("digest failed after all retries, giving up", "slot", greeting)
			return
		}

		slog.Info("retrying digest", "in", n.cfg.RetryInterval, "nextAttempt", attempt+1)
		select {
		case <-time.After(n.cfg.RetryInterval):
		case <-ctx.Done():
			return
		}
	}
}

// nextScheduledTime returns the next morning or evening schedule time and the
// greeting word ("morning" or "evening") to use in the digest.
func (n *Notifier) nextScheduledTime(now time.Time) (time.Time, string) {
	loc := now.Location()
	y, m, d := now.Date()

	candidates := []struct {
		t        time.Time
		greeting string
	}{
		{time.Date(y, m, d, n.cfg.MorningHour, 0, 0, 0, loc), "morning"},
		{time.Date(y, m, d, n.cfg.NoonHour, 0, 0, 0, loc), "afternoon"},
		{time.Date(y, m, d, n.cfg.EveningHour, 0, 0, 0, loc), "evening"},
	}

	for _, c := range candidates {
		if c.t.After(now) {
			return c.t, c.greeting
		}
	}

	// All of today's slots have passed — return tomorrow's morning.
	tomorrow := now.AddDate(0, 0, 1)
	ty, tm, td := tomorrow.Date()
	return time.Date(ty, tm, td, n.cfg.MorningHour, 0, 0, 0, loc), "morning"
}

func (n *Notifier) SendDigest(ctx context.Context, greeting string) error {
	return n.send(ctx, greeting, n.cfg.ChannelID, true)
}

// SendTestDigest sends a digest to channelID without marking articles as
// posted, so the production article queue is not affected.
func (n *Notifier) SendTestDigest(ctx context.Context, channelID int64) error {
	return n.send(ctx, n.currentGreeting(time.Now()), channelID, false)
}

// Repost sends a digest to the production channel and marks articles as posted.
// Intended for manual recovery after all automatic retry attempts have failed.
func (n *Notifier) Repost(ctx context.Context) error {
	return n.send(ctx, n.currentGreeting(time.Now()), n.cfg.ChannelID, true)
}

func (n *Notifier) currentGreeting(now time.Time) string {
	h := now.Hour()
	switch {
	case h < n.cfg.NoonHour:
		return "morning"
	case h < n.cfg.EveningHour:
		return "afternoon"
	default:
		return "evening"
	}
}

func (n *Notifier) send(ctx context.Context, greeting string, channelID int64, markPosted bool) error {
	since := time.Now().Add(-n.cfg.Lookback)
	articles, err := n.articles.AllNotPosted(ctx, since, uint64(n.cfg.MaxArticles))
	if err != nil {
		return fmt.Errorf("fetch articles: %w", err)
	}

	if len(articles) == 0 {
		slog.Info("no unposted articles for digest")
		return nil
	}

	slog.Info("building digest", "articles", len(articles), "slot", greeting, "channel", channelID, "markPosted", markPosted)

	grouped := groupByTheme(articles)
	digestInput := buildDigestInput(greeting, grouped, n.cfg.MaxInputDataLen)

	writeSummaryInput(n.cfg.SummaryInputDir, "digest.txt", digestInput)

	tokens, err := n.summarizer.CountTokens(digestInput)
	if err != nil {
		slog.Warn("summarizer.CountTokens", "err", err)
	}
	slog.Info("done digest input", "articles", len(articles), "slot", greeting, "channel", channelID, "tokens", tokens, "markPosted", markPosted)

	digestText, err := n.summarizer.Summarize(ctx, digestInput)
	if err != nil || strings.TrimSpace(digestText) == "" {
		if err != nil {
			slog.Error("digest summarization failed, using simple fallback", "err", err)
			n.reporter.Notify(fmt.Sprintf("Digest summarization error: %v", err))
		} else {
			slog.Warn("summarizer returned empty text, using simple fallback")
		}
		digestText = buildSimpleDigest(greeting, grouped)
	} else {
		writeSummaryInput(n.cfg.SummaryInputDir, "digest_output.txt", digestText)
		digestText = markup.SanitizeTelegramHTML(digestText)
	}

	chunks := markup.SplitMessage(digestText, markup.TelegramMessageLimit)
	for _, chunk := range chunks {
		if len(chunks) > 1 {
			// Re-balance tags that a chunk boundary may have split.
			chunk = markup.SanitizeTelegramHTML(chunk)
		}

		msg := tgbotapi.NewMessage(channelID, chunk)
		msg.ParseMode = "HTML"
		msg.DisableWebPagePreview = true

		if _, err := n.bot.Send(msg); err != nil {
			// Telegram rejects the whole message on any HTML parse error;
			// better to deliver the digest unformatted than not at all.
			slog.Warn("HTML digest send failed, retrying as plain text", "err", err)
			n.reporter.Notify(fmt.Sprintf("HTML digest send failed, falling back to plain text: %v", err))

			plain := tgbotapi.NewMessage(channelID, markup.StripHTML(chunk))
			plain.DisableWebPagePreview = true
			if _, err := n.bot.Send(plain); err != nil {
				return fmt.Errorf("send digest: %w", err)
			}
		}
	}

	if !markPosted {
		return nil
	}

	ids := make([]int64, len(articles))
	for i, article := range articles {
		ids[i] = article.ID
	}
	if err := n.articles.MarkAsPosted(ctx, ids); err != nil {
		slog.Error("mark as posted failed", "articleIDs", ids, "err", err)
	}

	return nil
}

// groupByTheme groups articles by their primary RSS category.
// Falls back to "General" when no category is set.
func groupByTheme(articles []model.Article) map[string][]model.Article {
	groups := make(map[string][]model.Article)
	for _, a := range articles {
		theme := "General"
		if len(a.Categories) > 0 && strings.TrimSpace(a.Categories[0]) != "" {
			theme = a.Categories[0]
		}
		groups[theme] = append(groups[theme], a)
	}
	return groups
}

// sortedThemes returns the group keys in a stable order so digest output does
// not depend on map iteration order.
func sortedThemes(grouped map[string][]model.Article) []string {
	themes := make([]string, 0, len(grouped))
	for theme := range grouped {
		themes = append(themes, theme)
	}
	sort.Strings(themes)
	return themes
}

// buildDigestInput constructs the structured text passed to the LLM.
func buildDigestInput(greeting string, grouped map[string][]model.Article, maxDataLen int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Time of day: %s\n\n", greeting))
	sb.WriteString("Articles by topic:\n\n")

	for _, theme := range sortedThemes(grouped) {
		sb.WriteString(fmt.Sprintf("Topic: %s\n", theme))
		for _, a := range grouped[theme] {
			summary := a.Summary
			if r := []rune(summary); len(r) > maxDataLen {
				summary = string(r[:maxDataLen]) + "..."
			}
			if summary != "" {
				sb.WriteString(fmt.Sprintf("- %s <%s> — %s\n", a.Title, a.Link, summary))
			} else {
				sb.WriteString(fmt.Sprintf("- %s <%s>\n", a.Title, a.Link))
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// writeSummaryInput saves the LLM input text to a file in dir for inspection.
// Errors are logged but do not affect the digest flow.
func writeSummaryInput(dir, filename, content string) {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Warn("failed to write summary input file", "path", path, "err", err)
	}
}

// buildSimpleDigest is a plain HTML fallback when the LLM is unavailable.
func buildSimpleDigest(greeting string, grouped map[string][]model.Article) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Good %s! Here's your tech news digest:\n\n", greeting))

	for _, theme := range sortedThemes(grouped) {
		sb.WriteString(fmt.Sprintf("<b>%s</b>\n", markup.EscapeForHTML(theme)))
		for _, a := range grouped[theme] {
			sb.WriteString(fmt.Sprintf("• <a href=\"%s\">%s</a>\n", a.Link, markup.EscapeForHTML(a.Title)))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
