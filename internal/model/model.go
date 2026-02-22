// Package model defines the data structures used in the newsMaker application, including Source, Item, and Article. These structures represent the sources of news, individual news items, and articles stored in the database, respectively.
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
	PublishedAt time.Time
	PostedAt    time.Time
	CreatedAt   time.Time
}
