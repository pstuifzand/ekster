package fetch

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The publication is a different url from the feed document, and a reader has
// to be handed the publication: the feed url is an XML document nobody wants to
// land on. FeedHeader carries it in HomePage, unsent, for the item source.
func TestFeedHeaderSeparatesTheSiteFromTheFeed(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0">
  <channel>
    <title>Zoetermeers Dagblad</title>
    <link>https://zoetermeersdagblad.nl/</link>
    <item><title>Een bericht</title><link>https://zoetermeersdagblad.nl/landelijk/een-bericht</link></item>
  </channel>
</rss>`

	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://zoetermeersdagblad.nl/rss", "application/rss+xml", strings.NewReader(rss))
	if assert.NoError(t, err) {
		assert.Equal(t, "https://zoetermeersdagblad.nl/rss", feed.URL, "url stays the feed the user followed")
		assert.Equal(t, "https://zoetermeersdagblad.nl/", feed.HomePage, "home page is the publication")
		assert.Equal(t, "Zoetermeers Dagblad", feed.Name)
	}
}

func TestFeedHeaderSiteForAFeedWithoutALink(t *testing.T) {
	rss := `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0"><channel><title>No link here</title></channel></rss>`

	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://example.com/feed.xml", "application/rss+xml", strings.NewReader(rss))
	if assert.NoError(t, err) {
		assert.Equal(t, "https://example.com/feed.xml", feed.HomePage, "a feed with no site link is its own site")
	}
}

func TestFeedHeaderSiteForAJSONFeed(t *testing.T) {
	doc := `{
	  "version": "https://jsonfeed.org/version/1.1",
	  "title": "A blog",
	  "home_page_url": "https://blog.example/",
	  "feed_url": "https://blog.example/feed.json",
	  "items": []
	}`

	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://blog.example/feed.json", "application/json", strings.NewReader(doc))
	if assert.NoError(t, err) {
		assert.Equal(t, "https://blog.example/feed.json", feed.URL)
		assert.Equal(t, "https://blog.example/", feed.HomePage)
	}
}

func TestFeedHeaderSiteForAnHTMLFeed(t *testing.T) {
	doc := `<html><head><title>Brad Enslen</title></head><body></body></html>`

	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://ramblinggit.com/", "text/html", strings.NewReader(doc))
	if assert.NoError(t, err) {
		assert.Equal(t, "https://ramblinggit.com/", feed.HomePage, "an HTML feed is its own publication page")
	}
}

// A host reads as a publication; a feed title reads as a channel label.
func TestHostName(t *testing.T) {
	assert.Equal(t, "zoetermeersdagblad.nl", HostName("https://zoetermeersdagblad.nl/landelijk/x"))
	assert.Equal(t, "bulletjournal.com", HostName("https://www.bulletjournal.com/blogs/bulletjournalist"))
	assert.Equal(t, "", HostName(""))
	assert.Equal(t, "", HostName("not a url"))
}
