package main

import (
	"context"
	"errors"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// A preview is the one fetch whose URL a person typed, and an empty url or a
// bare host is what the reader's feed box sends while it is still being filled
// in. The fetched URL is classified before any fetch happens, so this needs no
// pool and no database: the answer is "not a feed", not a 500.
func TestPreviewURLRejectsAURLThatCannotBeAFeed(t *testing.T) {
	backend := &memoryBackend{}

	for _, previewURL := range []string{"", "   ", "example.com", "example.com/feed.xml", "ftp://example.com/feed", "http://"} {
		_, err := backend.PreviewURL(context.Background(), previewURL)
		if !errors.Is(err, microsub.ErrNotAFeed) {
			t.Fatalf("PreviewURL(%q) err = %v, want ErrNotAFeed", previewURL, err)
		}
	}
}
