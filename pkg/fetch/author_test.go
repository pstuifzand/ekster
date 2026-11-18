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

	// An entry the feed wrote itself names nobody, so it has no author. The
	// feed is not a person: it used to be stamped here, which put a channel
	// title ("Hacker News - Newest: \"postgres\"") on every author line and,
	// later, a host in a person-shaped card. Which feed carried the entry is
	// item.Source's job, and a reader shows that when there is no byline.
	assert.Nil(t, items[1].Author)
}

// A Shopify-style blog names its feed "Site - Blog section" and gives each
// entry a person. An entry without one must not inherit that whole string.
const atomWithMixedAuthors = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <id>https://bulletjournal.com/blogs/bulletjournalist.atom</id>
  <title>Bullet Journal - The Bullet Journalist</title>
  <updated>2026-11-16T09:54:50-05:00</updated>
  <link rel="alternate" type="text/html" href="https://bulletjournal.com/blogs/bulletjournalist"/>
  <entry>
    <id>https://bulletjournal.com/blogs/bulletjournalist/doing-more</id>
    <title>Doing More vs. Doing What Matters</title>
    <link rel="alternate" type="text/html" href="https://bulletjournal.com/blogs/bulletjournalist/doing-more"/>
    <published>2026-11-16T09:54:50-05:00</published>
    <updated>2026-11-16T09:54:50-05:00</updated>
    <author><name>Lia Luzong</name></author>
  </entry>
  <entry>
    <id>https://bulletjournal.com/blogs/bulletjournalist/no-byline</id>
    <title>A post the blog published without a byline</title>
    <link rel="alternate" type="text/html" href="https://bulletjournal.com/blogs/bulletjournalist/no-byline"/>
    <published>2026-10-19T12:09:52-04:00</published>
    <updated>2026-10-22T12:45:30-04:00</updated>
  </entry>
</feed>`

func TestFeedItemsAuthorForAtomEntryWithoutOne(t *testing.T) {
	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://bulletjournal.com/blogs/bulletjournalist.atom", "application/atom+xml", strings.NewReader(atomWithMixedAuthors))

	if !assert.NoError(t, err) || !assert.Len(t, items, 2) {
		return
	}

	if assert.NotNil(t, items[0].Author) {
		assert.Equal(t, "Lia Luzong", items[0].Author.Name)
		assert.Empty(t, items[0].Author.URL)
	}

	// Same rule: no byline means no author card, not a publication wearing one.
	assert.Nil(t, items[1].Author)
}
