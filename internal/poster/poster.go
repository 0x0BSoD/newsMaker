// Package poster turns a single web page into a Telegram channel post:
// it extracts the readable article text, summarizes it with the LLM and
// publishes the result as Telegram HTML.
package poster

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit/markup"
)

const fetchTimeout = 30 * time.Second

type Summarizer interface {
	Summarize(ctx context.Context, text string) (string, error)
}

type Poster struct {
	summarizer    Summarizer
	bot           *tgbotapi.BotAPI
	channelID     int64
	maxContentLen int
	httpClient    *http.Client
}

func New(summarizer Summarizer, bot *tgbotapi.BotAPI, channelID int64, maxContentLen int) *Poster {
	return &Poster{
		summarizer:    summarizer,
		bot:           bot,
		channelID:     channelID,
		maxContentLen: maxContentLen,
		httpClient:    &http.Client{Timeout: fetchTimeout},
	}
}

// Prepare fetches pageURL, extracts the readable article text and asks the
// LLM to write a Telegram HTML post about it. The optional comment is passed
// to the LLM as an editor's note. The returned text is sanitized and ready
// to send.
func (p *Poster) Prepare(ctx context.Context, pageURL, comment string) (string, error) {
	title, text, err := p.extract(ctx, pageURL)
	if err != nil {
		return "", fmt.Errorf("extract article: %w", err)
	}

	input := buildInput(pageURL, title, comment, text, p.maxContentLen)

	out, err := p.summarizer.Summarize(ctx, input)
	if err != nil {
		return "", fmt.Errorf("summarize: %w", err)
	}
	if strings.TrimSpace(out) == "" {
		return "", fmt.Errorf("summarizer returned empty text")
	}

	out = markup.SanitizeTelegramHTML(out)

	// The post must always link back to the article, even if the LLM forgot.
	if !strings.Contains(out, pageURL) {
		out += fmt.Sprintf("\n\n<a href=\"%s\">Source</a>", html.EscapeString(pageURL))
	}

	return out, nil
}

// Publish sends prepared text to the production channel as Telegram HTML,
// falling back to plain text on HTML parse errors.
func (p *Poster) Publish(text string) error {
	return p.SendTo(p.channelID, text)
}

// SendTo sends prepared text to an arbitrary chat (used for previews).
func (p *Poster) SendTo(chatID int64, text string) error {
	chunks := markup.SplitMessage(text, markup.TelegramMessageLimit)
	for _, chunk := range chunks {
		if len(chunks) > 1 {
			// Re-balance tags that a chunk boundary may have split.
			chunk = markup.SanitizeTelegramHTML(chunk)
		}

		msg := tgbotapi.NewMessage(chatID, chunk)
		msg.ParseMode = "HTML"
		msg.DisableWebPagePreview = true

		if _, err := p.bot.Send(msg); err != nil {
			// Telegram rejects the whole message on any HTML parse error;
			// better to deliver unformatted than not at all.
			slog.Warn("HTML post send failed, retrying as plain text", "err", err)

			plain := tgbotapi.NewMessage(chatID, markup.StripHTML(chunk))
			plain.DisableWebPagePreview = true
			if _, err := p.bot.Send(plain); err != nil {
				return fmt.Errorf("send post: %w", err)
			}
		}
	}
	return nil
}

func (p *Poster) extract(ctx context.Context, pageURL string) (title, text string, err error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; newsMaker-bot/1.0)")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("page returned status %d", resp.StatusCode)
	}

	parser := readability.NewParser()
	article, err := parser.Parse(resp.Body, u)
	if err != nil {
		return "", "", err
	}

	var buf bytes.Buffer
	if err := article.RenderText(&buf); err != nil {
		return "", "", fmt.Errorf("render article text: %w", err)
	}

	return article.Title(), strings.TrimSpace(buf.String()), nil
}

func buildInput(pageURL, title, comment, text string, maxContentLen int) string {
	if r := []rune(text); len(r) > maxContentLen {
		text = string(r[:maxContentLen]) + "..."
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Article URL: %s\n", pageURL)
	if title != "" {
		fmt.Fprintf(&sb, "Title: %s\n", title)
	}
	if comment = strings.TrimSpace(comment); comment != "" {
		fmt.Fprintf(&sb, "Editor's note: %s\n", comment)
	}
	fmt.Fprintf(&sb, "\nContent:\n%s\n", text)

	return sb.String()
}
