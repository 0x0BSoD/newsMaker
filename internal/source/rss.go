// Package source implements the RSSSource struct and its methods for fetching and processing RSS feed items.
package source

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SlyMarbo/rss"
	"github.com/samber/lo"

	"github.com/0x0BSoD/newsMaker/internal/model"
)

const feedUserAgent = "Mozilla/5.0 (compatible; Feedfetcher-Google; +http://www.google.com/feedfetcher.html)"

// contextTransport injects a context and User-Agent into every outgoing
// request so that context cancellation, deadlines, and feed server
// bot-detection heuristics are handled correctly.
type contextTransport struct {
	ctx  context.Context
	base http.RoundTripper
}

func (t contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.WithContext(t.ctx)
	req.Header.Set("User-Agent", feedUserAgent)
	return t.base.RoundTrip(req)
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
			// Atom feeds (e.g. GitHub commit feeds) pad titles with newlines and indentation.
			Title:      strings.TrimSpace(item.Title),
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

	fetchFunc := func(u string) (*http.Response, error) {
		resp, err := client.Get(u)
		if err != nil {
			return nil, err
		}
		ct := resp.Header.Get("Content-Type")
		if strings.Contains(ct, "text/html") {
			resp.Body.Close()
			return nil, fmt.Errorf("server returned HTML instead of feed (status %d, Content-Type: %s) — may be blocking bots or requiring auth; URL: %s", resp.StatusCode, ct, u)
		}
		return resp, nil
	}

	return rss.FetchByFunc(fetchFunc, url)
}

func (s RSSSource) ID() int64 {
	return s.SourceID
}

func (s RSSSource) Name() string {
	return s.SourceName
}
