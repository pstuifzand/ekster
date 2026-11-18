package fetch

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// jsonFeedDoc is a one-entry JSON Feed 1.1 document. Its media type is part of
// what is under test, so every case below passes it explicitly.
const jsonFeedDoc = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "Bullet Journal - The Bullet Journalist",
  "home_page_url": "https://bulletjournal.com/blogs/bulletjournalist",
  "feed_url": "https://bulletjournal.com/blogs/bulletjournalist.json",
  "items": [
    {
      "id": "https://bulletjournal.com/blogs/bulletjournalist/no-byline",
      "url": "https://bulletjournal.com/blogs/bulletjournalist/no-byline",
      "title": "A post the blog published",
      "date_published": "2026-10-19T12:09:52-04:00"
    }
  ]
}`

// JSON Feed 1.1 moved to application/feed+json and kept application/json, and
// servers append a charset to either. Item parsing required both prefixes at
// once, so no media type satisfied it: the header parsed, and then every
// refresh logged "unknown content-type" and stored nothing.
func TestFeedItemsAcceptEveryJSONFeedMediaType(t *testing.T) {
	for _, contentType := range []string{
		"application/feed+json",
		"application/feed+json; charset=utf-8",
		"application/json",
		"application/json; charset=utf-8",
	} {
		items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://bulletjournal.com/blogs/bulletjournalist.json", contentType, strings.NewReader(jsonFeedDoc))
		if !assert.NoError(t, err, contentType) {
			continue
		}
		if assert.Len(t, items, 1, contentType) {
			assert.Equal(t, "A post the blog published", items[0].Name, contentType)
			assert.Equal(t, "entry", items[0].Type, contentType)
		}
	}
}

// The header answered only for application/json, so a feed served as
// application/feed+json previewed as an empty feed: no name, no site.
func TestFeedHeaderAcceptsFeedJSONMediaType(t *testing.T) {
	for _, contentType := range []string{"application/feed+json", "application/feed+json; charset=utf-8"} {
		feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://bulletjournal.com/blogs/bulletjournalist.json", contentType, strings.NewReader(jsonFeedDoc))
		if !assert.NoError(t, err, contentType) {
			continue
		}
		assert.Equal(t, "Bullet Journal - The Bullet Journalist", feed.Name, contentType)
		assert.Equal(t, "https://bulletjournal.com/blogs/bulletjournalist", feed.HomePage, contentType)
	}
}
