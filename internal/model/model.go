package model

import "time"

type Source struct {
	ID        int64
	Name      string
	FeedURL   string
	CreatedAt time.Time
}

type Item struct {
	Title      string
	Categories []string
	Link       string
	Date       time.Time
	Summary    string
	SourceName string
}

type Article struct {
	ID          int64
	SourceID    int64
	Title       string
	Link        string
	Summary     string
	PublishedAt string
	PostedAt    string
	CreatedAt   time.Time
}
