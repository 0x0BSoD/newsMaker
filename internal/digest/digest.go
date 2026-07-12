package digest

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"github.com/0x0BSoD/newsMaker/internal/botkit/markup"
	"github.com/0x0BSoD/newsMaker/internal/github"
	"github.com/0x0BSoD/newsMaker/internal/model"
	"github.com/0x0BSoD/newsMaker/internal/summary"
	"github.com/0x0BSoD/newsMaker/internal/telegraph"
)

// RepoStorage is satisfied by storage.GitHubRepoPostgresStorage.
type RepoStorage interface {
	Upsert(ctx context.Context, repos []model.GitHubRepo) (newCount int, err error)
	MarkPosted(ctx context.Context, fullNames []string) error
	LastPostedAt(ctx context.Context) (time.Time, bool, error)
	GetNewAndTrending(ctx context.Context, topic string, since time.Time, minGrowthPct float64) (newRepos []model.GitHubRepo, trending []model.GitHubRepo, err error)
}

// Config holds the non-dependency settings for a Digest.
type Config struct {
	ChannelID       int64
	Topics          []string
	Interval        time.Duration
	TopCount        int
	SummaryInputDir string
}

type Digest struct {
	gh         *github.Client
	tph        *telegraph.Client
	bot        *tgbotapi.BotAPI
	storage    RepoStorage
	summarizer summary.Summarizer
	cfg        Config
}

func New(
	gh *github.Client,
	tph *telegraph.Client,
	bot *tgbotapi.BotAPI,
	storage RepoStorage,
	summarizer summary.Summarizer,
	cfg Config,
) *Digest {
	return &Digest{
		gh:         gh,
		tph:        tph,
		bot:        bot,
		storage:    storage,
		summarizer: summarizer,
		cfg:        cfg,
	}
}

func (d *Digest) Start(ctx context.Context) error {
	if err := d.run(ctx); err != nil {
		slog.Error("digest run failed", "err", err)
	}

	ticker := time.NewTicker(d.cfg.Interval)
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
	newRepos []model.GitHubRepo
	trending []model.GitHubRepo
	pageURL  string
}

const (
	digestCooldown   = 7 * 24 * time.Hour
	minStarGrowthPct = 0.30 // 30% star growth to qualify as trending
)

func (d *Digest) run(ctx context.Context) error {
	if len(d.cfg.Topics) == 0 {
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

	return d.send(ctx, d.cfg.ChannelID, true)
}

// RunTest sends a digest to channelID without marking repos as posted, so the
// production cooldown and post state are not affected.
func (d *Digest) RunTest(ctx context.Context, channelID int64) error {
	if len(d.cfg.Topics) == 0 {
		return fmt.Errorf("no topics configured")
	}
	return d.send(ctx, channelID, false)
}

func (d *Digest) send(ctx context.Context, channelID int64, markPosted bool) error {
	slog.Info("running github digest", "topics", d.cfg.Topics, "channel", channelID, "markPosted", markPosted)

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
		results []topicResult
		// allFullNames collects every repo seen this run for MarkPosted.
		allFullNames []string
	)

	for _, topic := range d.cfg.Topics {
		topRepos, err := d.gh.GetByTopic(ctx, topic)
		if err != nil {
			slog.Error("GetByTopic failed", "topic", topic, "err", err)
		}

		recentRepos, err := d.gh.GetRecentByTopic(ctx, topic, 7)
		if err != nil {
			slog.Error("GetRecentByTopic failed", "topic", topic, "err", err)
		}

		topRepos = filterReadable(topRepos)
		recentRepos = filterReadable(recentRepos)

		// Upsert all fetched repos so star counts and language are up to date.
		all := dedup(topRepos, recentRepos)
		modelRepos := toModelRepos(all, topic)
		if _, err := d.storage.Upsert(ctx, modelRepos); err != nil {
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

		// Nothing changed for this topic — skip the Telegraph page.
		if len(newRepos) == 0 && len(trending) == 0 {
			continue
		}

		// Build Telegraph page for this topic (new + trending).
		nodes := buildTopicNodes(topic, newRepos, trending)
		pageTitle := fmt.Sprintf("%s — GitHub Digest %s", topic, time.Now().Format("2006-01-02"))
		pageURL, err := d.tph.CreatePage(pageTitle, nodes)
		if err != nil {
			slog.Error("create telegraph page failed", "topic", topic, "err", err)
			pageURL = ""
		}

		results = append(results, topicResult{
			topic:    topic,
			newRepos: newRepos,
			trending: trending,
			pageURL:  pageURL,
		})
	}

	top := selectTopRepos(results, d.cfg.TopCount)
	if len(top) == 0 {
		slog.Info("no new or trending repos this week, skipping digest")
		return nil
	}

	// One LLM call for the whole post: intro + per-repo Russian blurbs.
	input := buildWeeklyTopInput(top)
	summary.WriteDebugFile(d.cfg.SummaryInputDir, "trending.txt", input)
	post, err := d.summarizer.Summarize(ctx, input)
	if err != nil {
		slog.Error("summarize failed, falling back to plain repo list", "err", err)
		post = ""
	}
	summary.WriteDebugFile(d.cfg.SummaryInputDir, "trending_output.txt", post)

	msgData := buildTelegramMessage(post, top, results)
	chunks := markup.SplitMessage(msgData, markup.TelegramMessageLimit)
	for _, chunk := range chunks {
		if len(chunks) > 1 {
			// Re-balance tags that a chunk boundary may have split.
			chunk = markup.SanitizeTelegramHTML(chunk)
		}

		msg := tgbotapi.NewMessage(channelID, chunk)
		msg.ParseMode = "HTML"
		msg.DisableWebPagePreview = true

		if _, err := d.bot.Send(msg); err != nil {
			// Better to deliver the digest unformatted than not at all.
			slog.Warn("HTML digest send failed, retrying as plain text", "err", err)

			plain := tgbotapi.NewMessage(channelID, markup.StripHTML(chunk))
			plain.DisableWebPagePreview = true
			if _, err := d.bot.Send(plain); err != nil {
				return fmt.Errorf("send telegram message: %w", err)
			}
		}
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

// weeklyDelta is the star gain since the last digest. Repos without a
// snapshot (new this week) count all their stars.
// ponytail: favors big new arrivals over steady growers; tune if the top
// list gets dominated by old repos newly matching the search.
func weeklyDelta(r model.GitHubRepo) int {
	if r.StarsAtLastDigest != nil && *r.StarsAtLastDigest > 0 {
		return r.Stars - *r.StarsAtLastDigest
	}
	return r.Stars
}

// selectTopRepos pools new and trending repos across all topics, dedups by
// full name, and returns the n repos with the biggest weekly star delta.
func selectTopRepos(results []topicResult, n int) []model.GitHubRepo {
	seen := make(map[string]bool)
	var pool []model.GitHubRepo
	for _, res := range results {
		for _, r := range slices.Concat(res.newRepos, res.trending) {
			if !seen[r.FullName] {
				seen[r.FullName] = true
				pool = append(pool, r)
			}
		}
	}
	slices.SortFunc(pool, func(a, b model.GitHubRepo) int {
		return weeklyDelta(b) - weeklyDelta(a)
	})
	if n > 0 && len(pool) > n {
		pool = pool[:n]
	}
	return pool
}

// buildWeeklyTopInput renders the top repos as LLM input for the weekly post.
func buildWeeklyTopInput(repos []model.GitHubRepo) string {
	var sb strings.Builder
	sb.WriteString("Top GitHub repositories this week:\n")
	for _, r := range repos {
		lang := r.Language
		if lang == "" {
			lang = "unknown"
		}
		sb.WriteString(fmt.Sprintf("- %s | %s | %d stars (+%d this week) | %s | topic: %s\n  %s\n",
			r.FullName, r.HTMLURL, r.Stars, weeklyDelta(r), lang, r.Topic, r.Description))
	}
	return sb.String()
}

// buildTelegramMessage assembles the weekly post: the LLM-written body (or a
// plain repo list when the LLM failed) plus Telegraph links per topic.
func buildTelegramMessage(post string, top []model.GitHubRepo, results []topicResult) string {
	var sb strings.Builder

	if post != "" {
		sb.WriteString(markup.SanitizeTelegramHTML(post))
	} else {
		sb.WriteString(fmt.Sprintf("<b>GitHub Digest — Week of %s</b>\n", time.Now().Format("2006-01-02")))
		for _, r := range top {
			sb.WriteString(fmt.Sprintf("\n• <a href=\"%s\">%s</a> ⭐%d (+%d)\n", r.HTMLURL, r.FullName, r.Stars, weeklyDelta(r)))
			if r.Description != "" {
				sb.WriteString(r.Description + "\n")
			}
		}
	}

	var links []string
	for _, r := range results {
		if r.pageURL != "" {
			links = append(links, fmt.Sprintf("<a href=\"%s\">%s</a>", r.pageURL, r.topic))
		}
	}
	if len(links) > 0 {
		sb.WriteString("\n\nПодробнее: " + strings.Join(links, " · "))
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

func toModelRepos(repos []github.Repo, topic string) []model.GitHubRepo {
	out := make([]model.GitHubRepo, len(repos))
	for i, r := range repos {
		out[i] = model.GitHubRepo{
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

func repoLine(r model.GitHubRepo) string {
	lang := r.Language
	if lang == "" {
		lang = "unknown"
	}
	return fmt.Sprintf("%s ⭐%d | %s", r.FullName, r.Stars, lang)
}

func repoItem(r model.GitHubRepo, extra string) telegraph.Node {
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

func buildTopicNodes(topic string, newRepos, trending []model.GitHubRepo) []telegraph.Node {
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
