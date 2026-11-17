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

// The feed is the source. An entry that carries only a title stays a title:
// ekster must not read the permalink to find content the feed did not send.
func TestThinEntriesAreNotFetched(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)

	items, err := ProcessSourcedItems(context.Background(), fetcher, "https://example.com/feed", "text/html", strings.NewReader(feedPage))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}

	item := items[0]
	if counts["https://example.com/post/1"] != 0 {
		t.Errorf("the permalink was read %d times, want none", counts["https://example.com/post/1"])
	}
	if item.Content != nil && (item.Content.Text != "" || item.Content.HTML != "") {
		t.Errorf("content appeared from somewhere: %#v", item.Content)
	}
	if item.Name != "Identity Assertion JWT Authorization Grant Draft 06" {
		t.Errorf("the feed's title was not kept: %q", item.Name)
	}
	if len(item.Category) != 1 || item.Category[0] != "oauth" {
		t.Errorf("categories = %v, want just the feed's", item.Category)
	}
}

func TestAuthorReferenceIsResolvedToTheCard(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)

	items, err := ProcessSourcedItems(context.Background(), fetcher, "https://example.com/feed", "text/html", strings.NewReader(entryPage))
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

func TestInteractionReferenceIsFetchedAsMicroformat(t *testing.T) {
	const target = "https://example.com/post/1"
	counts := map[string]int{}
	item := microsub.Item{Type: "entry", LikeOf: []string{target}}
	got := enrichReferenceItems(context.Background(), fakeFetcher(pages(), counts), item)
	if counts[target] != 1 {
		t.Fatalf("target fetched %d times, want once", counts[target])
	}
	ref, ok := got.Refs[target]
	if !ok {
		t.Fatalf("reference for %s missing: %#v", target, got.Refs)
	}
	if ref.Name != "Identity Assertion JWT Authorization Grant Draft 06" {
		t.Errorf("reference name = %q", ref.Name)
	}
	if ref.Content == nil || !strings.Contains(ref.Content.Text, "draft -06") {
		t.Errorf("reference content = %#v", ref.Content)
	}
}

func TestInteractionReferenceDoesNotRefetchEmbeddedData(t *testing.T) {
	const target = "https://example.com/post/1"
	counts := map[string]int{}
	item := microsub.Item{
		LikeOf: []string{target},
		Refs:   map[string]microsub.Item{target: {Name: "Embedded"}},
	}
	got := enrichReferenceItems(context.Background(), fakeFetcher(pages(), counts), item)
	if counts[target] != 0 {
		t.Errorf("embedded target fetched %d times, want none", counts[target])
	}
	if got.Refs[target].Name != "Embedded" {
		t.Errorf("embedded reference was replaced: %#v", got.Refs[target])
	}
}

// Twenty entries by one person must not cost twenty profile fetches.
func TestResolvedCardsAreCached(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)
	resolver := newCardResolver(fetcher)

	for i := 0; i < 5; i++ {
		item := microsub.Item{Author: &microsub.Card{Type: "card", URL: "https://example.com/"}}
		item = resolveAuthor(context.Background(), resolver, item)
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

	items, err := ProcessSourcedItems(context.Background(), fetcher, "https://example.com/feed", "text/html", strings.NewReader(entryPage))
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
	base := fakeFetcher(map[string]string{"https://example.com/feed": entryPage}, nil)
	boom := fetch.FetcherFunc(func(ctx context.Context, u string) (*http.Response, error) {
		if u == "https://example.com/" {
			panic("parser exploded")
		}
		return base(ctx, u)
	})

	items, err := ProcessSourcedItems(context.Background(), boom, "https://example.com/feed", "text/html", strings.NewReader(entryPage))
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

// A feed of titles costs one request: the feed itself. Nothing follows links.
func TestTitleOnlyFeedMakesNoRequests(t *testing.T) {
	counts := map[string]int{}
	fetcher := fakeFetcher(pages(), counts)

	items, err := ProcessSourcedItems(context.Background(), fetcher, "https://example.com/feed", "text/html", strings.NewReader(feedPage))
	if err != nil {
		t.Fatalf("a title-only feed must not fail: %s", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	for url, n := range counts {
		if n != 0 {
			t.Errorf("%s was fetched %d times, want none", url, n)
		}
	}
}

// The head of a live Squarespace page (keningzhu.com/journal/contact-me-ripple):
// no microformats anywhere, only the metadata it publishes about itself. Before
// the fallback, itemAtURL answered "publishes no entries" and a thread showed
// one ancestor as missing instead of the post it replies to.
const pageWithoutMicroformats = `<!DOCTYPE html>
<html>
<head>
  <title>contact me: send me a ripple &mdash; kening zhu</title>
  <meta property="og:title" content="contact me: send me a ripple &mdash; kening zhu"/>
  <meta property="og:description" content="on making a new contact me page - and web interactions that feel more human, real, and imaginative."/>
  <meta property="og:image" content="https://static1.squarespace.com/static/51e8148de4b01c1eb79c1977/7.21.23_contact_me.jpg"/>
  <meta name="description" content=" on making a new contact me page  " />
</head>
<body><div class="sqs-block-content"><p>a page with no h-entry</p></div></body>
</html>`

func TestPageWithoutMicroformatsBecomesACard(t *testing.T) {
	const target = "https://keningzhu.com/journal/contact-me-ripple"
	fetcher := fakeFetcher(map[string]string{target: pageWithoutMicroformats}, nil)

	item, err := itemAtURL(context.Background(), fetcher, target)
	if err != nil {
		t.Fatalf("itemAtURL: %v", err)
	}

	// Entities are decoded by the parser; the name is the real one.
	if want := "contact me: send me a ripple — kening zhu"; item.Name != want {
		t.Errorf("name = %q, want %q", item.Name, want)
	}
	if item.Content == nil || !strings.Contains(item.Content.Text, "web interactions that feel more human") {
		t.Errorf("content = %#v", item.Content)
	}
	if len(item.Photo) != 1 || !strings.Contains(item.Photo[0], "static1.squarespace.com") {
		t.Errorf("photo = %#v", item.Photo)
	}
	if item.URL != target {
		t.Errorf("url = %q, want %q", item.URL, target)
	}
	// A description is text, not markup: the reader must never get HTML from
	// a page it did not parse as a post.
	if item.Content != nil && strings.ContainsAny(item.Content.Text, "<>") {
		t.Errorf("content carries markup: %q", item.Content.Text)
	}
}

func TestPageWithOnlyATitleBecomesACard(t *testing.T) {
	const target = "https://example.com/plain"
	fetcher := fakeFetcher(map[string]string{
		target: `<html><head><title>Just a title</title></head><body>no entry</body></html>`,
	}, nil)

	item, err := itemAtURL(context.Background(), fetcher, target)
	if err != nil {
		t.Fatalf("itemAtURL: %v", err)
	}
	if item.Name != "Just a title" {
		t.Errorf("name = %q", item.Name)
	}
	if item.Content != nil {
		t.Errorf("content should be absent, got %#v", item.Content)
	}
}

func TestEmptyPageIsStillMissing(t *testing.T) {
	const target = "https://example.com/empty"
	fetcher := fakeFetcher(map[string]string{
		target: `<html><head></head><body></body></html>`,
	}, nil)

	if _, err := itemAtURL(context.Background(), fetcher, target); err == nil {
		t.Fatal("a page with nothing to say must still be an error, not an empty item")
	}
}
