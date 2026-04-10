// Package sites provides article-page parsers for specific websites.
// Each parser knows how to extract title, categories, and a summary from
// a single article page. Parsers self-register via init() functions and
// are looked up by hostname at runtime.
package sites

import (
	"context"
	"net/url"
)

// Article holds the structured data extracted from a single article page.
type Article struct {
	Title      string
	Categories []string
	Summary    string
}

// Parser extracts structured article data from a website it knows about.
type Parser interface {
	// Host returns the hostname this parser handles (e.g. "platformengineering.org").
	Host() string
	// Parse fetches and parses the article at the given URL.
	Parse(ctx context.Context, articleURL string) (Article, error)
}

var registry []Parser

func register(p Parser) {
	registry = append(registry, p)
}

// Find returns the Parser registered for the given URL's hostname, or nil if none match.
func Find(rawURL string) Parser {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil
	}
	host := u.Hostname()
	for _, p := range registry {
		if p.Host() == host {
			return p
		}
	}
	return nil
}
