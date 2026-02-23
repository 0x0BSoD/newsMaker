// Package source implements the RSSSource struct and its methods for fetching and processing RSS feed items.
package source

import (
	"context"
	"crypto/tls"
	"net/http"
	"strings"
	"time"

	"github.com/SlyMarbo/rss"
	"github.com/samber/lo"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

// contextTransport injects a context into every outgoing request so that
// context cancellation and deadlines propagate through the rss library.
type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req.WithContext(t.ctx))
}

type RSSSource struct {
	URL        string
	SourceID   int64
	SourceName string
	Insecure   bool
}

func NewRSSSourceFromModel(m model.Source) RSSSource {
	return RSSSource{
		URL:        m.FeedURL,
		SourceID:   m.ID,
		SourceName: m.Name,
		Insecure:   m.Insecure,
	}
}

func (s RSSSource) Fetch(ctx context.Context) ([]model.Item, error) {
	feed, err := s.loadFeed(ctx, s.URL)
	if err != nil {
		return nil, err
	}

	return lo.Map(feed.Items, func(item *rss.Item, _ int) model.Item {
		return model.Item{
			Title:      item.Title,
			Categories: item.Categories,
			Link:       item.Link,
			Date:       item.Date,
			SourceName: s.SourceName,
			Summary:    itemText(item),
		}
	}), nil
}

// itemText returns the richest available text for an item.
// Content (full body) is preferred over Summary (short excerpt); falling back
// to Summary avoids an extra HTTP fetch in the notifier for feeds that omit Content.
func itemText(item *rss.Item) string {
	if c := strings.TrimSpace(item.Content); c != "" {
		return c
	}
	return strings.TrimSpace(item.Summary)
}

func (s RSSSource) loadFeed(ctx context.Context, url string) (*rss.Feed, error) {
	base := http.DefaultTransport
	if s.Insecure {
		base = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		}
	}
	client := &http.Client{
		Transport: contextTransport{ctx: ctx, base: base},
		Timeout:   30 * time.Second,
	}
	return rss.FetchByClient(url, client)
}

func (s RSSSource) ID() int64 {
	return s.SourceID
}

func (s RSSSource) Name() string {
	return s.SourceName
}
