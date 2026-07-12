package fetcher

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/0x0BSoD/newsMaker/internal/model"
	"github.com/0x0BSoD/newsMaker/internal/reporter"
	src "github.com/0x0BSoD/newsMaker/internal/source"
)

//go:generate moq --out=mocks/mock_article_storage.go --pkg=mocks . ArticleStorage
type ArticleStorage interface {
	Store(ctx context.Context, article model.Article) error
	LinkExists(ctx context.Context, link string) (bool, error)
}

// ItemEnricher is implemented by sources that can augment a feed item with
// additional details (extra API calls) before it is stored.
type ItemEnricher interface {
	Enrich(ctx context.Context, item model.Item) model.Item
}

//go:generate moq --out=mocks/mock_sources_provider.go --pkg=mocks . SourcesProvider
type SourcesProvider interface {
	Sources(ctx context.Context) ([]model.Source, error)
}

//go:generate moq --out=mocks/mock_source.go --pkg=mocks . Source
type Source interface {
	ID() int64
	Name() string
	Fetch(ctx context.Context) ([]model.Item, error)
}

type Fetcher struct {
	articles ArticleStorage
	sources  SourcesProvider
	reporter *reporter.Reporter
	// github is passed to source constructors that need GitHub API access
	// (KEPSource, CVESource); Fetcher itself never calls it.
	github src.GitHubClient

	fetchInterval  time.Duration
	filterKeywords []string
}

func New(
	articleStorage ArticleStorage,
	sourcesProvider SourcesProvider,
	fetchInterval time.Duration,
	filterKeywords []string,
	rep *reporter.Reporter,
	githubClient src.GitHubClient,
) *Fetcher {
	return &Fetcher{
		articles:       articleStorage,
		sources:        sourcesProvider,
		reporter:       rep,
		github:         githubClient,
		fetchInterval:  fetchInterval,
		filterKeywords: filterKeywords,
	}
}

func (f *Fetcher) Start(ctx context.Context) error {
	slog.Info("fetcher started")
	ticker := time.NewTicker(f.fetchInterval)
	defer ticker.Stop()

	if err := f.Fetch(ctx); err != nil {
		slog.Error("fetch failed", "err", err)
	}

	for {
		select {
		case <-ctx.Done():
			slog.Info("fetcher stopped")
			return ctx.Err()
		case <-ticker.C:
			if err := f.Fetch(ctx); err != nil {
				slog.Error("fetch failed", "err", err)
			}
		}
	}
}

func (f *Fetcher) Fetch(ctx context.Context) error {
	sources, err := f.sources.Sources(ctx)
	if err != nil {
		return err
	}

	const sourceTimeout = 60 * time.Second

	var wg sync.WaitGroup

	for _, source := range sources {
		wg.Add(1)

		s, err := f.newSource(source)
		if err != nil {
			slog.Error("source init failed", "source", source.Name, "err", err)
			f.reporter.Notify(fmt.Sprintf("Source init error [%s]: %v", source.Name, err))
			wg.Done()
			continue
		}

		go func(source Source) {
			defer wg.Done()

			sourceCtx, cancel := context.WithTimeout(ctx, sourceTimeout)
			defer cancel()

			items, err := source.Fetch(sourceCtx)
			if err != nil {
				slog.Error("source fetch failed", "source", source.Name(), "err", err)
				f.reporter.Notify(fmt.Sprintf("Fetch error [%s]: %v", source.Name(), err))
				return
			}

			if err := f.processItems(sourceCtx, source, items); err != nil {
				slog.Error("source process failed", "source", source.Name(), "err", err)
				f.reporter.Notify(fmt.Sprintf("Process error [%s]: %v", source.Name(), err))
				return
			}
		}(s)
	}

	wg.Wait()

	return nil
}

// maxEnrichPerCycle caps enrichment API calls per source per fetch cycle.
// Items over the cap are not stored and are picked up on a later cycle.
const maxEnrichPerCycle = 10

func (f *Fetcher) processItems(ctx context.Context, source Source, items []model.Item) error {
	var failed int
	var lastErr error

	enricher, canEnrich := source.(ItemEnricher)
	var enriched int

	for _, item := range items {
		item.Date = item.Date.UTC()

		if f.itemShouldBeSkipped(item) {
			continue
		}

		if canEnrich {
			exists, err := f.articles.LinkExists(ctx, item.Link)
			switch {
			case err != nil:
				// Storage hiccup: store unenriched rather than lose the item.
				slog.Warn("link existence check failed, skipping enrichment", "source", source.Name(), "link", item.Link, "err", err)
			case exists:
				continue
			case enriched >= maxEnrichPerCycle:
				continue
			default:
				item = enricher.Enrich(ctx, item)
				enriched++
			}
		}

		if err := f.articles.Store(ctx, model.Article{
			SourceID:    source.ID(),
			Title:       item.Title,
			Link:        item.Link,
			Summary:     item.Summary,
			Categories:  item.Categories,
			PublishedAt: item.Date,
		}); err != nil {
			// Keep going: one bad item should not drop the rest of the feed.
			slog.Error("store article failed", "source", source.Name(), "link", item.Link, "err", err)
			failed++
			lastErr = err
		}
	}

	if failed > 0 {
		return fmt.Errorf("store failed for %d of %d items, last error: %w", failed, len(items), lastErr)
	}

	return nil
}

func (f *Fetcher) itemShouldBeSkipped(item model.Item) bool {
	title := strings.ToLower(item.Title)

	categories := make(map[string]struct{}, len(item.Categories))
	for _, c := range item.Categories {
		categories[strings.ToLower(c)] = struct{}{}
	}

	for _, keyword := range f.filterKeywords {
		kw := strings.ToLower(keyword)
		if _, ok := categories[kw]; ok || strings.Contains(title, kw) {
			return true
		}
	}

	return false
}

func (f *Fetcher) newSource(m model.Source) (Source, error) {
	switch m.SourceType {
	case model.SourceTypeWeb:
		return src.NewWebSourceFromModel(m)
	case model.SourceTypeK8sKEP:
		return src.NewKEPSourceFromModel(m, f.github), nil
	case model.SourceTypeK8sCVE:
		return src.NewCVESourceFromModel(m, f.github), nil
	default:
		return src.NewRSSSourceFromModel(m), nil
	}
}
