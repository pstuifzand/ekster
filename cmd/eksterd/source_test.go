package main

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// What a reader is told about where an entry came from. The three values are
// three different things: the feed that was followed (and that an unfollow
// sends back), the publication a reader is handed, and the name that fits on a
// line next to a byline.
const zoetermeerFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Zoetermeers Dagblad</title>
    <link>https://zoetermeersdagblad.nl/</link>
    <description>Nieuws uit Zoetermeer</description>
    <image><url>https://zoetermeersdagblad.nl/logo.png</url><title>ZD</title><link>https://zoetermeersdagblad.nl/</link></image>
    <item>
      <title>Meerderheid voor PvdD-motie tegen duurdere treinkaartjes in 2027</title>
      <link>https://zoetermeersdagblad.nl/landelijk/meerderheid-voor-pvdd-motie</link>
      <description>Een Kamermeerderheid wil dat de NS de verhoging terugdraait.</description>
      <pubDate>Wed, 18 Nov 2026 18:18:00 +0000</pubDate>
      <dc:creator>Rene Dons</dc:creator>
    </item>
    <item>
      <title>Een bericht zonder byline</title>
      <link>https://zoetermeersdagblad.nl/landelijk/zonder-byline</link>
      <description>De krant schreef dit zelf.</description>
      <pubDate>Wed, 18 Nov 2026 17:00:00 +0000</pubDate>
    </item>
  </channel>
</rss>`

func TestProcessSourcedItemsNamesThePublication(t *testing.T) {
	const feedURL = "https://zoetermeersdagblad.nl/rss"
	fetcher := fakeFetcher(map[string]string{feedURL: zoetermeerFeed}, nil)

	items, err := ProcessSourcedItems(context.Background(), fetcher, feedURL, "application/rss+xml", strings.NewReader(zoetermeerFeed))
	if !assert.NoError(t, err) || !assert.Len(t, items, 2) {
		return
	}

	source := items[0].Source
	if assert.NotNil(t, source) {
		assert.Equal(t, feedURL, source.ID, "_id is the feed an unfollow sends")
		assert.Equal(t, "https://zoetermeersdagblad.nl/", source.URL, "url is the publication a reader is handed")
		assert.Equal(t, "zoetermeersdagblad.nl", source.Name, "the name is the publication, not the feed title")
		assert.Equal(t, "https://zoetermeersdagblad.nl/logo.png", source.Photo)
	}

	// The entry's own byline stays a person.
	if assert.NotNil(t, items[0].Author) {
		assert.Equal(t, "Rene Dons", items[0].Author.Name)
	}

	// The second entry names nobody: no author card at all. The source above
	// is what a reader shows in its place.
	assert.Nil(t, items[1].Author)
}
