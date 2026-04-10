package model

import (
	"time"
)

type Item struct {
	Title      string
	Categories []string
	Link       string
	Date       time.Time
	Summary    string
	SourceName string
}

// ScraperConfig holds CSS-selector-based configuration for web scraping sources.
type ScraperConfig struct {
	// LinkSelector is a CSS selector that matches article link elements on the listing page.
	// Example: "a[href^='/blog/']"
	LinkSelector string `json:"link_selector"`
	// BaseURL is prepended to relative hrefs found by LinkSelector.
	// Example: "https://platformengineering.org"
	BaseURL string `json:"base_url"`
	// MaxArticles limits how many articles are processed per fetch (0 = no limit).
	// Keeps fetch time bounded when listing pages have many articles.
	MaxArticles int `json:"max_articles,omitempty"`
}

const (
	SourceTypeRSS = "rss"
	SourceTypeWeb = "web"
)

type Source struct {
	ID            int64
	Name          string
	FeedURL       string
	Priority      int
	Insecure      bool
	SourceType    string
	ScraperConfig *ScraperConfig
	CreatedAt     time.Time
}

type Article struct {
	ID          int64
	SourceID    int64
	Title       string
	Link        string
	Summary     string
	Categories  []string
	PublishedAt time.Time
	PostedAt    time.Time
	CreatedAt   time.Time
}
