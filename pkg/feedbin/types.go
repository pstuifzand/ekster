package feedbin

import (
	"time"
)

// Entry is a feedbin api entry
type Entry struct {
	ID        int64     `json:"id"`
	FeedID    int64     `json:"feed_id"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Author    string    `json:"author"`
	Content   string    `json:"content"`
	Summary   string    `json:"summary"`
	Published time.Time `json:"published"`
	CreatedAt time.Time `json:"created_at"`
}

// Feed is a feedbin api feed
type Feed struct {
	ID      int64  `json:"id,omitempty"`
	Title   string `json:"title"`
	FeedURL string `json:"feed_url"`
	SiteURL string `json:"site_url"`
}

// Tagging is a feedbin api tagging
type Tagging struct {
	ID     int64  `json:"id,omitempty"`
	FeedID int64  `json:"feed_id"`
	Name   string `json:"name"`
}
