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
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func fetcher(ctx context.Context, fetchURL string) (*http.Response, error) {
	return nil, nil
}

func TestFeedHeaderUsesPageTitleAsAuthorFallback(t *testing.T) {
	doc := `<html><head><title>Brad Enslen</title></head><body></body></html>`
	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://ramblinggit.com/", "text/html", strings.NewReader(doc))
	if assert.NoError(t, err) {
		assert.Equal(t, "Brad Enslen", feed.Author.Name)
		assert.Equal(t, "https://ramblinggit.com/", feed.Author.URL)
		assert.Equal(t, "card", feed.Author.Type)
	}
}

func TestFeedHeader(t *testing.T) {
	doc := `
<html>
<body>
<div class="h-card">
  <p class="p-name"><a href="/" class="u-url">Title</a></p>
  <img class="u-photo" src="profile.jpg" />
</div>
</body>
</html>
`
	feed, err := FeedHeader(context.Background(), FetcherFunc(fetcher), "https://example.com/", "text/html", strings.NewReader(doc))
	if assert.NoError(t, err) {
		assert.Equal(t, "feed", feed.Type)
		assert.Equal(t, "Title", feed.Name)
		assert.Equal(t, "https://example.com/", feed.URL)
		assert.Equal(t, "https://example.com/profile.jpg", feed.Photo)
	}
}

// A bookmark feed carries the whole URL as the entry id, and items."uid" is
// varchar(512). The hex of a 332-character URL is 664 characters, so the
// insert failed and the bookmark vanished from the timeline with only a driver
// error in the log. The uid has to fit, and it has to stay the same hex for
// entries that already fit.
func TestLongFeedItemIDFitsTheUIDColumn(t *testing.T) {
	long := "https://www.yahoo.com/news/x.html?guccounter=1&guce_referrer_sig=" + strings.Repeat("AQ", 140)
	doc := `<?xml version="1.0"?>
<rdf:RDF xmlns="http://purl.org/rss/1.0/" xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel rdf:about="http://pinboard.in">
    <title>Pinboard</title>
    <link>https://pinboard.in/u:someone/toread/</link>
  </channel>
  <item rdf:about="` + strings.ReplaceAll(long, "&", "&amp;") + `">
    <title>A bookmark</title>
    <link>` + strings.ReplaceAll(long, "&", "&amp;") + `</link>
    <dc:date>2021-11-21T22:21:01+00:00</dc:date>
  </item>
  <item rdf:about="https://example.com/short">
    <title>A short bookmark</title>
    <link>https://example.com/short</link>
    <dc:date>2021-11-22T22:21:01+00:00</dc:date>
  </item>
</rdf:RDF>`

	items, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://pinboard.example/rss", "text/xml", strings.NewReader(doc))
	if err != nil {
		t.Fatalf("FeedItems: %s", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	for _, item := range items {
		if len(item.ID) > uidMaxLen {
			t.Errorf("uid for %q is %d bytes, longer than items.uid's %d", item.URL, len(item.ID), uidMaxLen)
		}
	}
	if got := items[1].ID; got != "68747470733a2f2f6578616d706c652e636f6d2f73686f7274" {
		t.Errorf("short item uid = %q, want the same hex as before", got)
	}
	if !strings.HasPrefix(items[0].ID, "sha256:") {
		t.Errorf("long item uid = %q, want a sha256 fallback", items[0].ID)
	}
}

// A re-fetch of an unchanged entry must dedupe, so the fallback has to be a
// function of the id and nothing else.
func TestHashedUIDIsStable(t *testing.T) {
	long := strings.Repeat("x", 400)
	first := itemUID(long)
	if first != itemUID(long) {
		t.Errorf("itemUID(%q) is not stable: %q then %q", long, first, itemUID(long))
	}
	if first == itemUID(long+"y") {
		t.Errorf("two different ids share the uid %q", first)
	}
}
// Mentions are what the markup says they are. Listing them must not turn into a
// request per link: that is what made one feed refresh look like a scraper.
func TestMentionsAreListedWithoutFetching(t *testing.T) {
	fetcher := FetcherFunc(func(ctx context.Context, u string) (*http.Response, error) {
		t.Errorf("a mention fetch was attempted for %s", u)
		return nil, errors.New("no fetch expected")
	})

	doc := `<div class="h-entry">
  <h2 class="p-name">An entry</h2>
  <a class="u-url" href="https://example.com/post/1">permalink</a>
  <div class="e-content">
    <a href="https://example.com/a">a</a>
    <a href="https://example.com/b">b</a>
    <a href="https://example.com/a">a again</a>
  </div>
</div>`

	items, err := FeedItems(context.Background(), fetcher, "https://example.com/feed", "text/html", strings.NewReader(doc))
	if err != nil {
		t.Fatalf("FeedItems: %s", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	got := items[0].MentionOf
	if len(got) != 2 {
		t.Fatalf("MentionOf = %v, want the two distinct links", got)
	}
	for _, href := range got {
		ref, ok := items[0].Refs[href]
		if !ok {
			t.Errorf("%s has no ref", href)
			continue
		}
		if ref.URL != href {
			t.Errorf("ref url = %q, want %q", ref.URL, href)
		}
		if ref.Content != nil && ref.Content.HTML != "" {
			t.Errorf("ref for %s carries content the fetch never happened for", href)
		}
	}
}
