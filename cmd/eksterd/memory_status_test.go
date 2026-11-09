package main

import (
	"testing"
	"time"
)

func TestClassifyFeed(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-24 * time.Hour)
	old := now.Add(-200 * 24 * time.Hour)
	zero := 0
	ten := 10

	tests := []struct {
		name          string
		failed        bool
		totalItems    int
		lastItemCount *int
		newestItemAt  *time.Time
		want          string
	}{
		// A fetch error wins over everything: the feed is broken now, whatever
		// it managed to store before.
		{name: "fetch failed", failed: true, totalItems: 100, lastItemCount: &ten, newestItemAt: &recent, want: "failing"},
		{name: "fetch failed, never had items", failed: true, totalItems: 0, want: "failing"},
		// Fetched fine, parsed into nothing: an h-feed the parser no longer
		// matches is invisible unless this is its own state.
		{name: "parsed zero items", totalItems: 0, lastItemCount: &zero, want: "empty"},
		// Never produced an item at all, even though the fetch works.
		{name: "never had items", totalItems: 0, want: "empty"},
		{name: "recent items", totalItems: 20, lastItemCount: &ten, newestItemAt: &recent, want: "ok"},
		{name: "old items", totalItems: 20, lastItemCount: &ten, newestItemAt: &old, want: "stale"},
		{name: "no items, no newest item", totalItems: 0, lastItemCount: &ten, want: "empty"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyFeed(tt.failed, tt.totalItems, tt.lastItemCount, tt.newestItemAt, now)
			if got != tt.want {
				t.Errorf("classifyFeed() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAge(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		at   time.Time
		want string
	}{
		{now.Add(-10 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-3 * time.Hour), "3h ago"},
		{now.Add(-72 * time.Hour), "3d ago"},
		{now.Add(time.Hour), "in the future"},
	}
	for _, tt := range tests {
		if got := age(tt.at, now); got != tt.want {
			t.Errorf("age(%s) = %q, want %q", tt.at, got, tt.want)
		}
	}
}
