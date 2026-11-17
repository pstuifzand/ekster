/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package fetch

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Shaped like a hnrss.org feed: one entry names its submitter in dc:creator,
// one does not.
const rssWithCreators = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Hacker News - Newest: "postgres"</title>
    <link>https://news.ycombinator.com/</link>
    <description>links</description>
    <image><url>https://news.ycombinator.com/favicon.ico</url><title>HN</title><link>https://news.ycombinator.com/</link></image>
    <item>
      <title>Postgres 19 is late</title>
      <description>Points: 172</description>
      <pubDate>Sat, 31 Oct 2026 01:38:56 +0000</pubDate>
      <link>http://rhaas.blogspot.com/postgres-19</link>
      <guid>https://news.ycombinator.com/item?id=45701234</guid>
      <dc:creator>yonasb</dc:creator>
    </item>
    <item>
      <title>An entry the feed wrote itself</title>
      <description>Points: 3</description>
      <pubDate>Sat, 31 Oct 2026 09:00:00 +0000</pubDate>
      <link>https://example.com/anonymous</link>
      <guid>https://news.ycombinator.com/item?id=45709999</guid>
    </item>
  </channel>
</rss>`

func TestFeedItemsAuthorComesFromTheEntry(t *testing.T) {
	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://hnrss.org/newest?q=postgres", "application/rss+xml", strings.NewReader(rssWithCreators))

	if !assert.NoError(t, err) || !assert.Len(t, items, 2) {
		return
	}

	// The person who submitted the link, not the publication that served it.
	if assert.NotNil(t, items[0].Author) {
		assert.Equal(t, "yonasb", items[0].Author.Name)
		assert.Equal(t, "card", items[0].Author.Type, "every author card carries its type")
		assert.Empty(t, items[0].Author.URL, "the feed link belongs to the publication, not to the submitter")
		assert.Empty(t, items[0].Author.Photo)
	}

	// With nothing per-entry to go on, the feed's own identity is the author,
	// which is how every feed without per-item authors has always rendered.
	if assert.NotNil(t, items[1].Author) {
		assert.Equal(t, `Hacker News - Newest: "postgres"`, items[1].Author.Name)
		assert.Equal(t, "https://news.ycombinator.com/", items[1].Author.URL)
	}
}
