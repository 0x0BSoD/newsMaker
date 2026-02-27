package notifier

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit/markup"
	"github.com/0x0BSoD/newsMaker/internal/model"
	"github.com/0x0BSoD/newsMaker/internal/reporter"
)

type ArticleProvider interface {
	AllNotPosted(ctx context.Context, since time.Time, limit uint64) ([]model.Article, error)
	MarkAsPosted(ctx context.Context, article model.Article) error
}

type SourcesProvider interface {
	Sources(ctx context.Context) ([]model.Source, error)
	SourceByID(ctx context.Context, id int64) (*model.Source, error)
}

type Summarizer interface {
	Summarize(text string) (string, error)
}

type Notifier struct {
	articles         ArticleProvider
	sources          SourcesProvider
	summarizer       Summarizer
	bot              *tgbotapi.BotAPI
	reporter         *reporter.Reporter
	sendInterval     time.Duration
	lookupTimeWindow time.Duration
	channelID        int64
}

func New(
	articleProvider ArticleProvider,
	sourcesProvider SourcesProvider,
	summarizer Summarizer,
	bot *tgbotapi.BotAPI,
	sendInterval time.Duration,
	lookupTimeWindow time.Duration,
	channelID int64,
	rep *reporter.Reporter,
) *Notifier {
	return &Notifier{
		articles:         articleProvider,
		sources:          sourcesProvider,
		summarizer:       summarizer,
		bot:              bot,
		reporter:         rep,
		sendInterval:     sendInterval,
		lookupTimeWindow: lookupTimeWindow,
		channelID:        channelID,
	}
}

func (n *Notifier) Start(ctx context.Context) error {
	slog.Info("notifier started")

	ticker := time.NewTicker(n.sendInterval)
	defer ticker.Stop()

	if err := n.SelectAndSendArticle(ctx); err != nil {
		return err
	}

	for {
		select {
		case <-ticker.C:
			if err := n.SelectAndSendArticle(ctx); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (n *Notifier) SelectAndSendArticle(ctx context.Context) error {
	topOneArticles, err := n.articles.AllNotPosted(ctx, time.Now().Add(-n.lookupTimeWindow), 1)
	if err != nil {
		return err
	}

	if len(topOneArticles) == 0 {
		return nil
	}

	article := topOneArticles[0]
	slog.Info("posting article", "title", article.Title)

	summary, err := n.extractSummary(ctx, article)
	if err != nil {
		slog.Error("summary extraction failed", "title", article.Title, "err", err)
		n.reporter.Notify(fmt.Sprintf("Summary error [%s]: %v", article.Title, err))
	}

	if err := n.sendArticle(article, summary); err != nil {
		return err
	}

	return n.articles.MarkAsPosted(ctx, article)
}

var redundantNewLines = regexp.MustCompile(`\n{3,}`)

func (n *Notifier) extractSummary(ctx context.Context, article model.Article) (string, error) {
	var r io.Reader

	if article.Summary != "" {
		r = strings.NewReader(article.Summary)
	} else {
		fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, article.Link, nil)
		if err != nil {
			return "", err
		}

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()

		r = resp.Body
	}

	doc, err := readability.FromReader(r, nil)
	if err != nil {
		return "", err
	}

	summary, err := n.summarizer.Summarize(cleanupText(doc.TextContent))
	if err != nil {
		return "", err
	}

	return "\n\n" + summary, nil
}

func cleanupText(text string) string {
	return redundantNewLines.ReplaceAllString(text, "\n")
}

// buildTags constructs the hashtag line: source name followed by up to 3
// category tags. Spaces in category names are replaced with underscores so
// the result is a valid Telegram hashtag.
func buildTags(sourceName string, categories []string) string {
	tags := "\\#" + markup.EscapeForMarkdown(sourceName)

	count := 0
	for _, cat := range categories {
		if count >= 3 {
			break
		}
		tag := strings.ReplaceAll(strings.TrimSpace(cat), " ", "_")
		if tag == "" {
			continue
		}
		tags += " \\#" + markup.EscapeForMarkdown(tag)
		count++
	}

	return tags
}

func (n *Notifier) sendArticle(article model.Article, summary string) error {
	const msgFormat = "*%s*%s\n\n%s\n%s"

	source, err := n.sources.SourceByID(context.Background(), article.SourceID)
	if err != nil {
		slog.Error("source lookup failed", "sourceID", article.SourceID, "articleID", article.ID, "err", err)
		source = &model.Source{Name: "unknown"}
	}

	msg := tgbotapi.NewMessage(n.channelID, fmt.Sprintf(
		msgFormat,
		markup.EscapeForMarkdown(article.Title),
		markup.EscapeForMarkdown(summary),
		markup.EscapeForMarkdown(article.Link),
		buildTags(source.Name, article.Categories),
	))
	msg.ParseMode = "MarkdownV2"

	_, err = n.bot.Send(msg)
	if err != nil {
		return err
	}

	return nil
}
