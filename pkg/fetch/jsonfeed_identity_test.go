package fetch

import (
	"context"
	"strings"
	"testing"

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
