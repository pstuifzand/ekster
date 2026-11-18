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

// jsonFeedAuthorsDoc has three entries: one that names its author in the 1.1
// authors array with no url, one 1.0-style author with a url, and one that
// names nobody at all.
const jsonFeedAuthorsDoc = `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "Bullet Journal - The Bullet Journalist",
  "home_page_url": "https://bulletjournal.com/blogs/bulletjournalist",
  "authors": [{"name": "Bullet Journal", "url": "https://bulletjournal.com/"}],
  "items": [
    {"id": "https://bulletjournal.com/one", "url": "https://bulletjournal.com/one", "title": "One",
     "date_published": "2026-11-16T09:54:50-05:00", "authors": [{"name": "Lia Luzong"}]},
    {"id": "https://bulletjournal.com/two", "url": "https://bulletjournal.com/two", "title": "Two",
     "date_published": "2026-11-15T09:54:50-05:00",
     "author": {"name": "Amanda Spalding", "url": "https://bulletjournal.com/author/amanda"}},
    {"id": "https://bulletjournal.com/three", "url": "https://bulletjournal.com/three", "title": "Three",
     "date_published": "2026-11-14T09:54:50-05:00"}
  ]
}`

// The array is where JSON Feed 1.1 says who wrote an entry, and the singular
// object is its 1.0 form. The parser knew neither.
func TestFeedItemsReadBothAuthorShapes(t *testing.T) {
	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://bulletjournal.com/blogs/bulletjournalist.json", "application/feed+json", strings.NewReader(jsonFeedAuthorsDoc))
	if !assert.NoError(t, err) || !assert.Len(t, items, 3) {
		return
	}

	if assert.NotNil(t, items[0].Author, "a name-only author in the authors array is still a byline") {
		assert.Equal(t, "Lia Luzong", items[0].Author.Name)
		assert.Equal(t, "card", items[0].Author.Type)
		assert.Empty(t, items[0].Author.URL)
	}

	if assert.NotNil(t, items[1].Author) {
		assert.Equal(t, "Amanda Spalding", items[1].Author.Name)
		assert.Equal(t, "https://bulletjournal.com/author/amanda", items[1].Author.URL)
	}

	// The feed names an author for entries that say nothing about themselves,
	// which is the fallback JSON Feed defines; the title is never a byline.
	if assert.NotNil(t, items[2].Author) {
		assert.Equal(t, "Bullet Journal", items[2].Author.Name)
		assert.Equal(t, "https://bulletjournal.com/", items[2].Author.URL)
	}
}

// An entry that names nobody in a feed that also names nobody has no author,
// rather than a card holding the feed title.
func TestFeedItemsJSONWithoutAnyAuthor(t *testing.T) {
	doc := `{"version": "https://jsonfeed.org/version/1.1", "title": "A blog",
	  "home_page_url": "https://blog.example/", "items": [
	    {"id": "https://blog.example/one", "url": "https://blog.example/one", "title": "One",
	     "date_published": "2026-11-16T09:54:50-05:00"}]}`

	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://blog.example/feed.json", "application/json", strings.NewReader(doc))
	if !assert.NoError(t, err) || !assert.Len(t, items, 1) {
		return
	}
	assert.Nil(t, items[0].Author)
}

// The header reads the author too, for the follow/preview card.
func TestFeedHeaderReadsTheAuthorsArray(t *testing.T) {
	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://bulletjournal.com/blogs/bulletjournalist.json", "application/feed+json", strings.NewReader(jsonFeedAuthorsDoc))
	if !assert.NoError(t, err) {
		return
	}
	assert.Equal(t, "Bullet Journal", feed.Author.Name, "the feed's own author, not only the 1.0 field")
	assert.Equal(t, "https://bulletjournal.com/", feed.Author.URL)
}

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
