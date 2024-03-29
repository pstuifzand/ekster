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
	feed, err := FeedHeader(FetcherFunc(fetcher), "https://example.com/", "text/html", strings.NewReader(doc))
	if assert.NoError(t, err) {
		assert.Equal(t, "feed", feed.Type)
		assert.Equal(t, "Title", feed.Name)
		assert.Equal(t, "https://example.com/", feed.URL)
		assert.Equal(t, "https://example.com/profile.jpg", feed.Photo)
	}
}
