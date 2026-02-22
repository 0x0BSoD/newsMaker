package fetcher

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/0x0BSoD/newsMaker/internal/model"
	"github.com/0x0BSoD/newsMaker/internal/source"
)

type ArticleStorage interface {
	Store(ctx context.Context, article model.Article) error
}

type SourceProvider interface {
	Sources(ctx context.Context) ([]model.Source, error)
}

type Source interface {
	ID() int64
	Name() string
	Fetch(ctx context.Context) ([]model.Item, error)
}

type Fetcher struct {
	articles ArticleStorage
	sources  SourceProvider

	fetchInterval  time.Duration
	filterKeywords []string
}

func New(
	articles ArticleStorage,
	sources SourceProvider,
	fetchInterval time.Duration,
	filterKeywords []string,
) *Fetcher {
	return &Fetcher{
		articles:       articles,
		sources:        sources,
		fetchInterval:  fetchInterval,
		filterKeywords: filterKeywords,
	}
}

func (f *Fetcher) Start(ctx context.Context) error {
	ticker := time.NewTicker(f.fetchInterval)
	defer ticker.Stop()

	if err := f.Fetch(ctx); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := f.Fetch(ctx); err != nil {
				return err
			}
		}
	}
}

func (f *Fetcher) Fetch(ctx context.Context) error {
	sources, err := f.sources.Sources(ctx)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup

	for _, src := range sources {
		wg.Add(1)

		rssSource := source.NewRSSSourceFromModel(src)
		go func(source Source) {
			defer wg.Done()

			items, err := source.Fetch(ctx)
			if err != nil {
				log.Printf("[ERROR] failed to fetch items for source %d: %v", source.ID(), err)
				return
			}
			if err := f.processItems(ctx, source, items); err != nil {
				log.Printf("[ERROR] failed to process items for source %d: %v", source.ID(), err)
				return
			}
		}(rssSource)
	}
	wg.Wait()

	return nil
}

func makeSet(in []string) []string {
	m := map[string]bool{}
	var out []string
	for _, v := range in {
		if _, ok := m[v]; !ok {
			m[v] = true
			out = append(out, v)
		}
	}
	return out
}

func setContains(this string, in []string) bool {
	for _, v := range in {
		if v == this {
			return true
		}
	}
	return false
}

func (f *Fetcher) itemMustSkipped(item model.Item) bool {
	categoriesSet := makeSet(item.Categories)
	for _, keyword := range f.filterKeywords {
		titleContainsKeyword := strings.Contains(strings.ToLower(item.Title), keyword)
		if setContains(keyword, categoriesSet) || titleContainsKeyword {
			return true
		}
	}

	return false
}

func (f *Fetcher) processItems(ctx context.Context, source Source, items []model.Item) error {
	for _, item := range items {
		item.Date = time.Now().UTC()

		if f.itemMustSkipped(item) {
			continue
		}

		if err := f.articles.Store(ctx, model.Article{
			SourceID:    source.ID(),
			Title:       item.Title,
			Link:        item.Link,
			Summary:     item.Summary,
			PublishedAt: item.Date,
		}); err != nil {
			return err
		}
	}
	return nil
}
