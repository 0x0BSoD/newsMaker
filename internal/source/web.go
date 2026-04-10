package source

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/0x0BSoD/newsMaker/internal/model"
	"github.com/0x0BSoD/newsMaker/internal/source/sites"
)

const defaultMaxArticles = 10

// WebSource scrapes an HTML listing page, extracts article links with a CSS
// selector, then enriches each article via a registered site-specific parser.
type WebSource struct {
	URL         string
	SourceID    int64
	SourceName  string
	LinkSel     string
	BaseURL     string
	MaxArticles int
}

func NewWebSourceFromModel(m model.Source) (WebSource, error) {
	if m.ScraperConfig == nil {
		return WebSource{}, fmt.Errorf("web source %q has no scraper_config", m.Name)
	}
	max := m.ScraperConfig.MaxArticles
	if max <= 0 {
		max = defaultMaxArticles
	}
	return WebSource{
		URL:         m.FeedURL,
		SourceID:    m.ID,
		SourceName:  m.Name,
		LinkSel:     m.ScraperConfig.LinkSelector,
		BaseURL:     strings.TrimRight(m.ScraperConfig.BaseURL, "/"),
		MaxArticles: max,
	}, nil
}

func (s WebSource) ID() int64    { return s.SourceID }
func (s WebSource) Name() string { return s.SourceName }

func (s WebSource) Fetch(ctx context.Context) ([]model.Item, error) {
	urls, err := s.scrapeListingPage(ctx)
	if err != nil {
		return nil, err
	}

	items := make([]model.Item, 0, len(urls))
	for _, link := range urls {
		item := s.enrichArticle(ctx, link)
		items = append(items, item)
	}
	return items, nil
}

// scrapeListingPage fetches the listing URL and returns up to MaxArticles
// unique article links matched by LinkSel.
func (s WebSource) scrapeListingPage(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return nil, fmt.Errorf("web source %q: build request: %w", s.SourceName, err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; newsMaker-bot/1.0)")

	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("web source %q: fetch listing: %w", s.SourceName, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("web source %q: listing returned status %d", s.SourceName, resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web source %q: parse listing HTML: %w", s.SourceName, err)
	}

	seen := make(map[string]bool)
	var links []string

	doc.Find(s.LinkSel).EachWithBreak(func(_ int, sel *goquery.Selection) bool {
		href, exists := sel.Attr("href")
		if !exists || href == "" {
			return true
		}
		link := s.resolveLink(href)
		if seen[link] {
			return true
		}
		seen[link] = true
		links = append(links, link)
		return len(links) < s.MaxArticles
	})

	return links, nil
}

// enrichArticle calls the registered site parser for the article URL.
// Falls back to a minimal item (URL as title) if no parser is registered.
func (s WebSource) enrichArticle(ctx context.Context, link string) model.Item {
	item := model.Item{
		Link:       link,
		Date:       time.Now().UTC(),
		SourceName: s.SourceName,
	}

	parser := sites.Find(link)
	if parser == nil {
		// No dedicated parser — use the URL slug as a best-effort title.
		item.Title = slugToTitle(link)
		return item
	}

	parsed, err := parser.Parse(ctx, link)
	if err != nil {
		// Degrade gracefully: store the URL with slug title so dedup still works.
		item.Title = slugToTitle(link)
		return item
	}

	item.Title = parsed.Title
	item.Categories = parsed.Categories
	item.Summary = parsed.Summary
	return item
}

func (s WebSource) resolveLink(href string) string {
	if strings.HasPrefix(href, "http://") || strings.HasPrefix(href, "https://") {
		return href
	}
	if s.BaseURL == "" {
		return href
	}
	if !strings.HasPrefix(href, "/") {
		href = "/" + href
	}
	return s.BaseURL + href
}

// slugToTitle converts a URL path like "/blog/what-is-platform-engineering"
// into a readable string "what is platform engineering".
func slugToTitle(link string) string {
	parts := strings.Split(strings.TrimRight(link, "/"), "/")
	slug := parts[len(parts)-1]
	return strings.ReplaceAll(slug, "-", " ")
}
