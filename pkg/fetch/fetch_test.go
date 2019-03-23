package fetch

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func fetcher(fetchURL string) (*http.Response, error) {
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
	feed, err := FeedHeader(fetcher, "https://example.com/", "text/html", strings.NewReader(doc))
	if assert.NoError(t, err) {
		assert.Equal(t, "feed", feed.Type)
		assert.Equal(t, "Title", feed.Name)
		assert.Equal(t, "https://example.com/", feed.URL)
		assert.Equal(t, "https://example.com/profile.jpg", feed.Photo)
	}
}
