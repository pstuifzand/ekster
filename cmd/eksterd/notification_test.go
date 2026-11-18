package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A notification is an entry in the notifications channel, so its _source is
// drawn on the entry line like any other publication's. It carried the feed
// document's url as the name, which the reader rendered as a raw link where a
// publication belongs.
func TestNotificationSourceNamesThePublication(t *testing.T) {
	source := notificationSource(feed{ID: 24, URL: "https://news.ycombinator.com/rss"})

	if assert.NotNil(t, source) {
		assert.Equal(t, "news.ycombinator.com", source.Name)
		assert.Equal(t, "https://news.ycombinator.com/rss", source.URL, "the link still points at the feed that failed")
		assert.Equal(t, "24", source.ID)
	}
}

// A url with no host is all the notification has to name; it must not become
// an empty label.
func TestNotificationSourceFallsBackToTheFeedURL(t *testing.T) {
	source := notificationSource(feed{ID: 1, URL: "not a url"})
	assert.Equal(t, "not a url", source.Name)
}
