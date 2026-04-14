package digest

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit/markup"
	"github.com/0x0BSoD/newsMaker/internal/github"
	"github.com/0x0BSoD/newsMaker/internal/storage"
	"github.com/0x0BSoD/newsMaker/internal/telegraph"
)

// RepoStorage is satisfied by storage.GitHubRepoPostgresStorage.
type RepoStorage interface {
	Upsert(ctx context.Context, repos []storage.GitHubRepo) (newCount int, err error)
	MarkPosted(ctx context.Context, fullNames []string) error
	LastPostedAt(ctx context.Context) (time.Time, bool, error)
	GetNewAndTrending(ctx context.Context, topic string, since time.Time, minGrowthPct float64) (newRepos []storage.GitHubRepo, trending []storage.GitHubRepo, err error)
}

// Summarizer is satisfied by the Ollama / OpenAI summarizer implementations.
type Summarizer interface {
	Summarize(text string) (string, error)
	CountTokens(text string) (int, error)
}

type Digest struct {
	gh              *github.Client
	tph             *telegraph.Client
	bot             *tgbotapi.BotAPI
	storage         RepoStorage
	summarizer      Summarizer
	channelID       int64
	topics          []string
	interval        time.Duration
	summaryInputDir string
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
	summaryInputDir string,
) *Digest {
	return &Digest{
		gh:              gh,
		tph:             tph,
		bot:             bot,
		storage:         storage,
		summarizer:      summarizer,
		channelID:       channelID,
		topics:          topics,
		interval:        interval,
		summaryInputDir: summaryInputDir,
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
	newRepos []storage.GitHubRepo
	trending []storage.GitHubRepo
	pageURL  string
	summary  string
}

const (
	digestCooldown   = 7 * 24 * time.Hour
	minStarGrowthPct = 0.30 // 30% star growth to qualify as trending
)

func (d *Digest) run(ctx context.Context) error {
	if len(d.topics) == 0 {
		slog.Warn("no topics configured, skipping digest")
		return nil
	}

	lastPosted, ok, err := d.storage.LastPostedAt(ctx)
	if err != nil {
		return fmt.Errorf("check last posted: %w", err)
	}
	if ok && time.Since(lastPosted) < digestCooldown {
		slog.Info("digest already sent recently, skipping",
			"lastPosted", lastPosted,
			"nextIn", digestCooldown-time.Since(lastPosted).Truncate(time.Hour),
		)
		return nil
	}

	return d.send(ctx, d.channelID, true)
}

// RunTest sends a digest to channelID without marking repos as posted, so the
// production cooldown and post state are not affected.
func (d *Digest) RunTest(ctx context.Context, channelID int64) error {
	if len(d.topics) == 0 {
		return fmt.Errorf("no topics configured")
	}
	return d.send(ctx, channelID, false)
}

func (d *Digest) send(ctx context.Context, channelID int64, markPosted bool) error {
	slog.Info("running github digest", "topics", d.topics, "channel", channelID, "markPosted", markPosted)

	// Determine the baseline time for delta computation.
	lastPosted, hasLastPosted, err := d.storage.LastPostedAt(ctx)
	if err != nil {
		return fmt.Errorf("check last posted: %w", err)
	}
	var since time.Time
	if hasLastPosted {
		since = lastPosted
	}
	// since == zero means "no previous digest" → all repos will appear as new.

	var (
		results       []topicResult
		totalNew      int
		totalTrending int
		// allFullNames collects every repo seen this run for MarkPosted.
		allFullNames      []string
		trendingInputBuf  strings.Builder
		trendingOutputBuf strings.Builder
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

		// Upsert all fetched repos so star counts and language are up to date.
		all := dedup(topRepos, recentRepos)
		storageRepos := toStorageRepos(all, topic)
		if _, err := d.storage.Upsert(ctx, storageRepos); err != nil {
			slog.Error("upsert repos failed", "topic", topic, "err", err)
		}
		for _, r := range all {
			allFullNames = append(allFullNames, r.FullName)
		}

		// Fetch only the delta: new repos and trending (star-growth) repos.
		newRepos, trending, err := d.storage.GetNewAndTrending(ctx, topic, since, minStarGrowthPct)
		if err != nil {
			slog.Error("GetNewAndTrending failed", "topic", topic, "err", err)
		}

		// Build Telegraph page for this topic (new + trending).
		nodes := buildTopicNodes(topic, newRepos, trending)
		pageTitle := fmt.Sprintf("%s — GitHub Digest %s", topic, time.Now().Format("2006-01-02"))
		pageURL, err := d.tph.CreatePage(pageTitle, nodes)
		if err != nil {
			slog.Error("create telegraph page failed", "topic", topic, "err", err)
			pageURL = ""
		}

		// AI summary of changes.
		summaryInput := buildSummaryInput(topic, newRepos, trending)
		trendingInputBuf.WriteString(summaryInput)
		trendingInputBuf.WriteString("\n---\n\n")
		summary, err := d.summarizer.Summarize(summaryInput)
		if err != nil {
			slog.Error("summarize failed", "topic", topic, "err", err)
			summary = ""
		}
		trendingOutputBuf.WriteString(fmt.Sprintf("### %s\n\n%s\n\n---\n\n", topic, summary))

		results = append(results, topicResult{
			topic:    topic,
			newRepos: newRepos,
			trending: trending,
			pageURL:  pageURL,
			summary:  summary,
		})
		totalNew += len(newRepos)
		totalTrending += len(trending)
	}

	writeSummaryInput(d.summaryInputDir, "trending.txt", trendingInputBuf.String())
	writeSummaryInput(d.summaryInputDir, "trending_output.txt", trendingOutputBuf.String())

	msgData := buildTelegramMessage(results, totalNew, totalTrending, hasLastPosted)
	if msgData == "empty" {
		slog.Warn("buildTelegramMessage empty message")
		// Basically it is not an error, so just return
		return nil
	}
	msg := tgbotapi.NewMessage(channelID, msgData)
	msg.ParseMode = "HTML"
	msg.DisableWebPagePreview = true

	if _, err := d.bot.Send(msg); err != nil {
		return fmt.Errorf("send telegram message: %w", err)
	}

	if !markPosted {
		return nil
	}

	// Snapshot stars_at_last_digest for ALL seen repos so the next digest can
	// detect growth relative to this run, even for repos not in the delta.
	if err := d.storage.MarkPosted(ctx, allFullNames); err != nil {
		slog.Error("mark posted failed", "err", err)
	}

	return nil
}

func buildTelegramMessage(results []topicResult, totalNew, totalTrending int, hasLastPosted bool) string {
	var sb strings.Builder

	if hasLastPosted {
		sb.WriteString(fmt.Sprintf(
			"<b>GitHub Digest — Week of %s</b>\n\n%d new repos, %d trending across all topics.\n",
			time.Now().Format("2006-01-02"), totalNew, totalTrending,
		))
	} else {
		sb.WriteString(fmt.Sprintf(
			"<b>GitHub Digest — %s (first run)</b>\n\n%d repos discovered across all topics.\n",
			time.Now().Format("2006-01-02"), totalNew,
		))
	}

	lenResults := len(results)
	var resultsMissCounter int
	for _, r := range results {
		if len(r.newRepos) == 0 && len(r.trending) == 0 {
			// sb.WriteString(fmt.Sprintf("\n<b>%s:</b> no changes this week\n", r.topic))
			resultsMissCounter++
			continue
		}
		sb.WriteString(fmt.Sprintf("\n<b>%s:</b> %d new, %d trending\n", r.topic, len(r.newRepos), len(r.trending)))
		if r.summary != "" {
			sb.WriteString(markup.SanitizeTelegramHTML(r.summary) + "\n")
		}
		if r.pageURL != "" {
			sb.WriteString(fmt.Sprintf("\n<a href=\"%s\">View on Telegraph</a>\n", r.pageURL))
		}
	}

	if resultsMissCounter == lenResults {
		return "empty"
	}

	return sb.String()
}

func buildSummaryInput(topic string, newRepos, trending []storage.GitHubRepo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Topic: %s\n", topic))

	if len(newRepos) > 0 {
		sb.WriteString("\nNew repos:\n")
		for _, r := range newRepos {
			lang := r.Language
			if lang == "" {
				lang = "unknown"
			}
			sb.WriteString(fmt.Sprintf("- %s (%d stars, %s): %s\n", r.FullName, r.Stars, lang, r.Description))
		}
	}

	if len(trending) > 0 {
		sb.WriteString("\nTrending (significant star growth):\n")
		for _, r := range trending {
			lang := r.Language
			if lang == "" {
				lang = "unknown"
			}
			prev := 0
			if r.StarsAtLastDigest != nil {
				prev = *r.StarsAtLastDigest
			}
			var growthStr string
			if prev > 0 {
				pct := float64(r.Stars-prev) / float64(prev) * 100
				growthStr = fmt.Sprintf("+%.0f%%", pct)
			}
			sb.WriteString(fmt.Sprintf("- %s (%d stars %s, %s): %s\n", r.FullName, r.Stars, growthStr, lang, r.Description))
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
			Language:    r.Language,
			Description: r.Description,
			HTMLURL:     r.HTMLURL,
		}
	}
	return out
}

func repoLine(r storage.GitHubRepo) string {
	lang := r.Language
	if lang == "" {
		lang = "unknown"
	}
	return fmt.Sprintf("%s ⭐%d | %s", r.FullName, r.Stars, lang)
}

func repoItem(r storage.GitHubRepo, extra string) telegraph.Node {
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

// writeSummaryInput saves the LLM input text to a file in dir for inspection.
// Errors are logged but do not affect the digest flow.
func writeSummaryInput(dir, filename, content string) {
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		slog.Warn("failed to write summary input file", "path", path, "err", err)
	}
}

func buildTopicNodes(topic string, newRepos, trending []storage.GitHubRepo) []telegraph.Node {
	var nodes []telegraph.Node

	// New repos section
	nodes = append(nodes, telegraph.Node{Tag: "h3", Children: []any{topic + " — New This Week"}})
	var newItems []any
	for _, r := range newRepos {
		daysAgo := int(time.Since(r.FirstSeenAt).Hours() / 24)
		newItems = append(newItems, repoItem(r, fmt.Sprintf("🆕 first seen %d days ago", daysAgo)))
	}
	if len(newItems) == 0 {
		newItems = append(newItems, telegraph.Node{Tag: "li", Children: []any{"No new repos this week"}})
	}
	nodes = append(nodes, telegraph.Node{Tag: "ul", Children: newItems})

	// Trending repos section
	nodes = append(nodes, telegraph.Node{Tag: "h3", Children: []any{topic + " — Trending (Star Growth)"}})
	var trendingItems []any
	for _, r := range trending {
		extra := ""
		if r.StarsAtLastDigest != nil && *r.StarsAtLastDigest > 0 {
			prev := *r.StarsAtLastDigest
			pct := float64(r.Stars-prev) / float64(prev) * 100
			extra = fmt.Sprintf("📈 +%.0f%% (%d→%d stars)", pct, prev, r.Stars)
		}
		trendingItems = append(trendingItems, repoItem(r, extra))
	}
	if len(trendingItems) == 0 {
		trendingItems = append(trendingItems, telegraph.Node{Tag: "li", Children: []any{"No trending repos this week"}})
	}
	nodes = append(nodes, telegraph.Node{Tag: "ul", Children: trendingItems})

	return nodes
}
