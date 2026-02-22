package notifier

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-shiori/go-readability"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit/markup"
	"github.com/0x0BSoD/newsMaker/internal/model"
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
) *Notifier {
	return &Notifier{
		articles:         articleProvider,
		sources:          sourcesProvider,
		summarizer:       summarizer,
		bot:              bot,
		sendInterval:     sendInterval,
		lookupTimeWindow: lookupTimeWindow,
		channelID:        channelID,
	}
}

func (n *Notifier) Start(ctx context.Context) error {
	log.Printf("[INFO] Notifier started")

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
	log.Printf("[INFO] Selecting Article")
	topOneArticles, err := n.articles.AllNotPosted(ctx, time.Now().Add(-n.lookupTimeWindow), 1)
	if err != nil {
		return err
	}

	if len(topOneArticles) == 0 {
		return nil
	}

	article := topOneArticles[0]
	log.Printf("[INFO] Selected Article: %s", article.Title)

	log.Printf("[INFO] Extracting Summary")
	summary, err := n.extractSummary(article)
	if err != nil {
		log.Printf("[ERROR] failed to extract summary: %v", err)
	}

	if err := n.sendArticle(article, summary); err != nil {
		return err
	}

	return n.articles.MarkAsPosted(ctx, article)
}

var redundantNewLines = regexp.MustCompile(`\n{3,}`)

func (n *Notifier) extractSummary(article model.Article) (string, error) {
	var r io.Reader

	if article.Summary != "" {
		r = strings.NewReader(article.Summary)
	} else {
		resp, err := http.Get(article.Link)
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

func (n *Notifier) sendArticle(article model.Article, summary string) error {
	const msgFormat = "*%s*%s\n\n%s\n\\#%s"

	source, _ := n.sources.SourceByID(context.Background(), article.SourceID)

	msg := tgbotapi.NewMessage(n.channelID, fmt.Sprintf(
		msgFormat,
		markup.EscapeForMarkdown(article.Title),
		markup.EscapeForMarkdown(summary),
		markup.EscapeForMarkdown(article.Link),
		markup.EscapeForMarkdown(source.Name),
	))
	msg.ParseMode = "MarkdownV2"

	_, err := n.bot.Send(msg)
	if err != nil {
		return err
	}

	return nil
}
