package jsonfeed

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParse(t *testing.T) {
	f, err := os.Open("testdata/feed.json")
	assert.NoError(t, err)

	feed, err := Parse(f)
	assert.NoError(t, err)

	assert.Equal(t, "https://jsonfeed.org/version/1", feed.Version)
	assert.Equal(t, "JSON Feed", feed.Title)
	assert.Equal(t, "https://www.jsonfeed.org/", feed.HomePageURL)
	assert.Equal(t, "https://www.jsonfeed.org/feed.json", feed.FeedURL)

	assert.Len(t, feed.Items, 2)
	assert.Equal(t, "http://jsonfeed.micro.blog/2020/08/07/json-feed-version.html", feed.Items[0].ID)
	assert.Equal(t, "http://jsonfeed.micro.blog/2017/05/17/announcing-json-feed.html", feed.Items[1].ID)
}
