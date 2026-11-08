package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/fetch"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

// fakeFetcher serves pages by URL and counts how often each one was asked for.
func fakeFetcher(pages map[string]string, counts map[string]int) fetch.FetcherFunc {
	return func(ctx context.Context, u string) (*http.Response, error) {
		if counts != nil {
			counts[u]++
		}
		body, ok := pages[u]
		if !ok {
			return nil, fmt.Errorf("no page at %s", u)
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{"Content-Type": {"text/html; charset=UTF-8"}},
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	}
}

// A feed that lists only a title is a teaser. The entry page has the text, and
// the reader should show it rather than a headline over nothing.
const feedPage = `
<div class="h-entry">
  <h2 class="p-name">Identity Assertion JWT Authorization Grant Draft 06</h2>
  <a class="u-url" href="https://example.com/post/1">permalink</a>
  <span class="p-category">oauth</span>
</div>
`

const entryPage = `
<div class="h-entry">
  <h2 class="p-name">Identity Assertion JWT Authorization Grant Draft 06</h2>
  <a class="u-url" href="https://example.com/post/1">permalink</a>
  <a class="u-author" href="https://example.com/"></a>
  <div class="e-content"><p>We have just published draft -06.</p></div>
  <span class="p-category">ietf</span>
</div>
`

// The author is a card with a url and nothing else, which is jf2's reference
// form; the h-card at that url is what names them.
const profilePage = `
<div class="h-card">
  <a class="u-url p-name" href="https://example.com/">Example Author</a>
  <img class="u-photo" src="https://example.com/photo.jpg" alt="">
</div>
`

func pages() map[string]string {
	return map[string]string{
		"https://example.com/feed":   feedPage,
		"https://example.com/post/1": entryPage,
		"https://example.com/":       profilePage,
	}
}

func TestThinEntriesAreFilledFromThePermalink(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)

	items, err := ProcessSourcedItems(fetcher, "https://example.com/feed", "text/html", strings.NewReader(feedPage))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	item := items[0]
	if item.Content == nil || !strings.Contains(item.Content.Text, "draft -06") {
		t.Errorf("content was not filled in from the entry: %#v", item.Content)
	}
	if item.Name != "Identity Assertion JWT Authorization Grant Draft 06" {
		t.Errorf("the feed's title was not kept: %q", item.Name)
	}
	if counts["https://example.com/post/1"] != 1 {
		t.Errorf("the permalink was read %d times, want once", counts["https://example.com/post/1"])
	}
	// Categories from both the feed and the entry are kept.
	if len(item.Category) != 2 {
		t.Errorf("categories = %v, want both the feed and the entry's", item.Category)
	}
}

func TestAuthorReferenceIsResolvedToTheCard(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)

	items, err := ProcessSourcedItems(fetcher, "https://example.com/feed", "text/html", strings.NewReader(feedPage))
	if err != nil {
		t.Fatal(err)
	}
	author := items[0].Author
	if author == nil {
		t.Fatal("the entry has no author at all")
	}
	if author.Name != "Example Author" {
		t.Errorf("author name = %q, want the h-card's name", author.Name)
	}
	if author.Photo != "https://example.com/photo.jpg" {
		t.Errorf("author photo = %q, want the h-card's photo", author.Photo)
	}
	if author.URL != "https://example.com/" {
		t.Errorf("author url = %q, want the url it was read from", author.URL)
	}
}

// Twenty entries by one person must not cost twenty profile fetches.
func TestResolvedCardsAreCached(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)
	resolver := newCardResolver(fetcher)

	for i := 0; i < 5; i++ {
		item := microsub.Item{Author: &microsub.Card{Type: "card", URL: "https://example.com/"}}
		item = resolveAuthor(resolver, item)
		if item.Author.Name != "Example Author" {
			t.Fatalf("call %d: author name = %q", i, item.Author.Name)
		}
	}
	if counts["https://example.com/"] != 1 {
		t.Errorf("the profile was fetched %d times, want once", counts["https://example.com/"])
	}
}

// An entry that already says something is left alone: no extra requests.
func TestFullEntriesAreNotFetchedAgain(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)

	items, err := ProcessSourcedItems(fetcher, "https://example.com/feed", "text/html", strings.NewReader(entryPage))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if counts["https://example.com/post/1"] != 0 {
		t.Errorf("a full entry was fetched again %d times", counts["https://example.com/post/1"])
	}
}

// A page that makes the parser panic must cost one entry's enrichment, not the
// process. A nil dereference inside the microformats parser did exactly that in
// production while enriching an entry from a mention feed.
func TestPanickingEnrichmentKeepsTheItem(t *testing.T) {
	base := fakeFetcher(map[string]string{"https://example.com/feed": feedPage}, nil)
	boom := fetch.FetcherFunc(func(ctx context.Context, u string) (*http.Response, error) {
		if u == "https://example.com/post/1" {
			panic("parser exploded")
		}
		return base(ctx, u)
	})

	items, err := ProcessSourcedItems(boom, "https://example.com/feed", "text/html", strings.NewReader(feedPage))
	if err != nil {
		t.Fatalf("a panic while enriching one entry must not fail the feed: %s", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Name == "" {
		t.Errorf("the feed's title was lost")
	}
}

// A permalink that cannot be read leaves the item as the feed gave it.
func TestUnreachablePermalinkKeepsTheItem(t *testing.T) {
	fetcher := fakeFetcher(map[string]string{"https://example.com/feed": feedPage}, nil)

	items, err := ProcessSourcedItems(fetcher, "https://example.com/feed", "text/html", strings.NewReader(feedPage))
	if err != nil {
		t.Fatalf("an unreachable entry must not fail the feed: %s", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].Name == "" {
		t.Errorf("the feed's title was lost")
	}
}
