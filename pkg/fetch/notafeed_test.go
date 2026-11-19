package fetch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// A host that answers something no parser here reads is not a server failure of
// ekster's: the handler turns this into 400 so a preview of a text/plain URL
// says "not a feed" instead of "the server answered 500".
func TestFeedItemsUnknownContentTypeIsNotAFeed(t *testing.T) {
	_, err := FeedItems(context.Background(), FetcherFunc(fetcher), "https://example.com/notes.txt", "text/plain; charset=utf-8", strings.NewReader("just a note"))
	if !errors.Is(err, microsub.ErrNotAFeed) {
		t.Fatalf("err = %v, want ErrNotAFeed", err)
	}
}
