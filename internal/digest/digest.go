package digest

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/github"
	"github.com/0x0BSoD/newsMaker/internal/storage"
	"github.com/0x0BSoD/newsMaker/internal/telegraph"
)

// RepoStorage is satisfied by storage.GitHubRepoPostgresStorage.
type RepoStorage interface {
	Upsert(ctx context.Context, repos []storage.GitHubRepo) (newCount int, err error)
	MarkPosted(ctx context.Context, fullNames []string) error
}

// Summarizer is satisfied by the Ollama / OpenAI summarizer implementations.
type Summarizer interface {
	Summarize(text string) (string, error)
}

type Digest struct {
	gh         *github.Client
	tph        *telegraph.Client
	bot        *tgbotapi.BotAPI
	storage    RepoStorage
	summarizer Summarizer
	channelID  int64
	topics     []string
	interval   time.Duration
}

func New(
	gh *github.Client,
	tph *telegraph.Client,
	bot *tgbotapi.BotAPI,
	storage RepoStorage,
	summarizer Summarizer,
	channelID int64,
	topics []string,
	interval time.Duration,
) *Digest {
	return &Digest{
		gh:         gh,
		tph:        tph,
		bot:        bot,
		storage:    storage,
		summarizer: summarizer,
		channelID:  channelID,
		topics:     topics,
		interval:   interval,
	}
}

func (d *Digest) Start(ctx context.Context) error {
	if err := d.run(ctx); err != nil {
		slog.Error("digest run failed", "err", err)
	}

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := d.run(ctx); err != nil {
				slog.Error("digest run failed", "err", err)
			}
		}
	}
}

type topicResult struct {
	topic    string
	repos    []github.Repo
	newCount int
	pageURL  string
	summary  string
}

func (d *Digest) run(ctx context.Context) error {
	if len(d.topics) == 0 {
		slog.Warn("no topics configured, skipping digest")
		return nil
	}

	slog.Info("running github digest", "topics", d.topics)

	var (
		results    []topicResult
		totalRepos int
		totalNew   int
	)

	for _, topic := range d.topics {
		topRepos, err := d.gh.GetByTopic(topic)
		if err != nil {
			slog.Error("GetByTopic failed", "topic", topic, "err", err)
		}

		recentRepos, err := d.gh.GetRecentByTopic(topic, 7)
		if err != nil {
			slog.Error("GetRecentByTopic failed", "topic", topic, "err", err)
		}

		topRepos = filterReadable(topRepos)
		recentRepos = filterReadable(recentRepos)

		// Upsert all repos to DB (top + recent, deduplicated)
		all := dedup(topRepos, recentRepos)
		storageRepos := toStorageRepos(all, topic)
		newCount, err := d.storage.Upsert(ctx, storageRepos)
		if err != nil {
			slog.Error("upsert repos failed", "topic", topic, "err", err)
		}

		// Build Telegraph page for this topic
		nodes := buildTopicNodes(topic, topRepos, recentRepos)
		pageTitle := fmt.Sprintf("%s — GitHub Digest %s", topic, time.Now().Format("2006-01-02"))
		pageURL, err := d.tph.CreatePage(pageTitle, nodes)
		if err != nil {
			slog.Error("create telegraph page failed", "topic", topic, "err", err)
			pageURL = ""
		}

		// AI summary
		summaryInput := buildSummaryInput(topic, topRepos, recentRepos)
		summary, err := d.summarizer.Summarize(summaryInput)
		if err != nil {
			slog.Error("summarize failed", "topic", topic, "err", err)
			summary = ""
		}

		results = append(results, topicResult{
			topic:    topic,
			repos:    all,
			newCount: newCount,
			pageURL:  pageURL,
			summary:  summary,
		})
		totalRepos += len(all)
		totalNew += newCount
	}

	// Mark all repos as posted
	var allFullNames []string
	for _, r := range results {
		for _, repo := range r.repos {
			allFullNames = append(allFullNames, repo.FullName)
		}
	}
	if err := d.storage.MarkPosted(ctx, allFullNames); err != nil {
		slog.Error("mark posted failed", "err", err)
	}

	msg := tgbotapi.NewMessage(d.channelID, buildTelegramMessage(results, totalRepos, totalNew))
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true

	if _, err := d.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}

	return nil
}

func buildTelegramMessage(results []topicResult, totalRepos, totalNew int) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"<b>GitHub Digest — Week of %s</b>\n\nWe have %d repos for today and %d from them are new.\n",
		time.Now().Format("2006-01-02"), totalRepos, totalNew,
	))

	for _, r := range results {
		sb.WriteString(fmt.Sprintf("\n<b>%s:</b>\n", r.topic))
		if r.summary != "" {
			sb.WriteString(r.summary + "\n")
		}
		if r.pageURL != "" {
			sb.WriteString(fmt.Sprintf("\n<a href=\"%s\">View on Telegraph</a>\n", r.pageURL))
		}
	}

	return sb.String()
}

func buildSummaryInput(topic string, topRepos, recentRepos []github.Repo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Topic: %s\n\nTop repos:\n", topic))
	for _, r := range topRepos {
		sb.WriteString(fmt.Sprintf("- %s (%d stars, %s): %s\n", r.FullName, r.StargazersCount, r.Language, r.Description))
	}
	if len(recentRepos) > 0 {
		sb.WriteString("\nNew this week:\n")
		for _, r := range recentRepos {
			sb.WriteString(fmt.Sprintf("- %s (%d stars, %s): %s\n", r.FullName, r.StargazersCount, r.Language, r.Description))
		}
	}
	return sb.String()
}

// isReadable returns true if the text contains only Latin, Cyrillic, or
// script-neutral (digits, symbols, spaces) characters.
func isReadable(text string) bool {
	for _, r := range text {
		if r > 0x7E && // outside printable ASCII
			!(r >= 0x0400 && r <= 0x04FF) && // Cyrillic
			!(r >= 0x00C0 && r <= 0x024F) && // Latin Extended
			!(r >= 0x2000 && r <= 0x206F) && // General Punctuation
			!(r >= 0x2600 && r <= 0x27BF) && // Misc symbols / emoji ranges
			!(r >= 0x1F300 && r <= 0x1FAFF) { // Emoji
			return false
		}
	}
	return true
}

func filterReadable(repos []github.Repo) []github.Repo {
	out := repos[:0]
	for _, r := range repos {
		if isReadable(r.FullName) && isReadable(r.Description) {
			out = append(out, r)
		}
	}
	return out
}

// dedup merges topRepos and recentRepos, keeping each full_name once.
func dedup(top, recent []github.Repo) []github.Repo {
	seen := make(map[string]bool, len(top)+len(recent))
	result := make([]github.Repo, 0, len(top)+len(recent))
	for _, r := range append(top, recent...) {
		if !seen[r.FullName] {
			seen[r.FullName] = true
			result = append(result, r)
		}
	}
	return result
}

func toStorageRepos(repos []github.Repo, topic string) []storage.GitHubRepo {
	out := make([]storage.GitHubRepo, len(repos))
	for i, r := range repos {
		out[i] = storage.GitHubRepo{
			FullName:    r.FullName,
			Topic:       topic,
			Stars:       r.StargazersCount,
			Description: r.Description,
			HTMLURL:     r.HTMLURL,
		}
	}
	return out
}

func repoLine(r github.Repo) string {
	lang := r.Language
	if lang == "" {
		lang = "unknown"
	}
	return fmt.Sprintf("%s ⭐%d | %s", r.FullName, r.StargazersCount, lang)
}

func repoItem(r github.Repo, extra string) telegraph.Node {
	linkText := repoLine(r)
	if extra != "" {
		linkText += " " + extra
	}
	children := []any{
		telegraph.Node{
			Tag:      "a",
			Attrs:    map[string]string{"href": r.HTMLURL},
			Children: []any{linkText},
		},
	}
	if r.Description != "" {
		children = append(children,
			telegraph.Node{Tag: "br"},
			telegraph.Node{
				Tag:      "i",
				Children: []any{r.Description},
			},
		)
	}
	return telegraph.Node{Tag: "li", Children: children}
}

func buildTopicNodes(topic string, topRepos, recentRepos []github.Repo) []telegraph.Node {
	var nodes []telegraph.Node

	// Top repos section
	nodes = append(nodes, telegraph.Node{Tag: "h3", Children: []any{topic + " — Top Repos"}})
	var topItems []any
	for _, r := range topRepos {
		topItems = append(topItems, repoItem(r, ""))
	}
	if len(topItems) == 0 {
		topItems = append(topItems, telegraph.Node{Tag: "li", Children: []any{"No results"}})
	}
	nodes = append(nodes, telegraph.Node{Tag: "ul", Children: topItems})

	// New this week section
	nodes = append(nodes, telegraph.Node{Tag: "h3", Children: []any{topic + " — New This Week"}})
	var recentItems []any
	for _, r := range recentRepos {
		daysAgo := int(time.Since(r.CreatedAt).Hours() / 24)
		recentItems = append(recentItems, repoItem(r, fmt.Sprintf("🆕 created %d days ago", daysAgo)))
	}
	if len(recentItems) == 0 {
		recentItems = append(recentItems, telegraph.Node{Tag: "li", Children: []any{"No new repos this week"}})
	}
	nodes = append(nodes, telegraph.Node{Tag: "ul", Children: recentItems})

	return nodes
}
