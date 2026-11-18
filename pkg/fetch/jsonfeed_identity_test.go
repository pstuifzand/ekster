package fetch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// An id is what the store keys a row by, and JSON Feed's is required by the
// spec but missing often enough. The url is the entry's permalink and the
// microformats branch already uses it as the uid; without the fallback every
// id-less entry became the empty uid and they all collapsed into one row.
func TestFeedItemsJSONEntryWithoutIDUsesItsURL(t *testing.T) {
	doc := `{"version": "https://jsonfeed.org/version/1.1", "title": "A blog",
	  "home_page_url": "https://blog.example/", "items": [
	    {"url": "https://blog.example/one", "title": "One", "content_text": "x"},
	    {"id": "https://blog.example/two", "url": "https://blog.example/two", "title": "Two", "content_text": "y"},
	    {"title": "No id and no url", "content_text": "z"}]}`

	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://blog.example/feed.json", "application/feed+json", strings.NewReader(doc))
	if !assert.NoError(t, err) || !assert.Len(t, items, 2, "an entry with neither id nor url is not addressable") {
		return
	}
	assert.Equal(t, itemUID("https://blog.example/one"), items[0].ID, "the permalink is the identity")
	assert.NotEmpty(t, items[0].ID)
	assert.Equal(t, itemUID("https://blog.example/two"), items[1].ID)
}

// date_published and date_modified are both optional, and the store rejects an
// empty published_at: an undated entry was dropped with one log line per
// refresh instead of being kept.
func TestFeedItemsJSONEntryWithoutADate(t *testing.T) {
	doc := `{"version": "https://jsonfeed.org/version/1.1", "title": "A blog",
	  "home_page_url": "https://blog.example/", "items": [
	    {"id": "https://blog.example/published", "title": "Published", "date_published": "2026-11-16T09:54:50-05:00"},
	    {"id": "https://blog.example/modified", "title": "Modified only", "date_modified": "2026-11-17T10:00:00Z"},
	    {"id": "https://blog.example/undated", "title": "Undated"},
	    {"id": "https://blog.example/nonsense", "title": "Nonsense date", "date_published": "last tuesday"}]}`

	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://blog.example/feed.json", "application/feed+json", strings.NewReader(doc))
	if !assert.NoError(t, err) || !assert.Len(t, items, 4) {
		return
	}

	assert.Equal(t, "2026-11-16T09:54:50-05:00", items[0].Published)
	assert.Equal(t, "2026-11-17T10:00:00Z", items[1].Published, "date_modified is the fallback the spec names")
	assert.Equal(t, time.Time{}.Format(time.RFC3339), items[2].Published, "an undated entry keeps the place the RSS path gives it")
	assert.Equal(t, time.Time{}.Format(time.RFC3339), items[3].Published, "an unparseable date must not cost the entry")

	for _, item := range items {
		if assert.NotEmpty(t, item.Published, item.Name) {
			_, err := time.Parse(time.RFC3339, item.Published)
			assert.NoError(t, err, item.Name)
		}
	}
}
