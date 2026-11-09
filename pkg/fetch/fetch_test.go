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
