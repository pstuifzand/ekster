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

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/pstuifzand/ekster/pkg/fetch"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

// maxPageSize bounds how much of a page is read while filling in an item.
const maxPageSize = 4 << 20

// cardTTL is how long a resolved author card is reused. A name and a photo do
// not change often, and a feed with twenty entries by the same person should
// cost one request, not twenty.
const cardTTL = 24 * time.Hour

// page is a fetched document and how its server said to read it.
type page struct {
	body        []byte
	contentType string
}

// readPage fetches a URL and reads at most maxPageSize of it.
func readPage(ctx context.Context, fetcher fetch.Fetcher, u string) (page, error) {
	if !isHTTPURL(u) {
		return page{}, fmt.Errorf("not an http url: %q", u)
	}
	resp, err := fetcher.FetchWithContext(ctx, u)
	if err != nil {
		return page{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return page{}, fmt.Errorf("GET %s: %s", u, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPageSize))
	if err != nil {
		return page{}, err
	}
	return page{body: body, contentType: resp.Header.Get("Content-Type")}, nil
}

// isHTTPURL says whether a string is something that can be fetched.
func isHTTPURL(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

type resolvedCard struct {
	card microsub.Card
	at   time.Time
}

// cardResolver turns an author that is only a reference into the h-card it
// points at. jf2 says a reference to another object may be the URL where that
// object lives instead of an inline card; expanding it is the consumer's job,
// and the difference is a name in the reader instead of "unknown".
type cardResolver struct {
	fetcher fetch.Fetcher
	now     func() time.Time
	mu      sync.Mutex
	cards   map[string]resolvedCard
}

func newCardResolver(fetcher fetch.Fetcher) *cardResolver {
	return &cardResolver{
		fetcher: fetcher,
		now:     time.Now,
		cards:   map[string]resolvedCard{},
	}
}

// Card returns the h-card at u, cached for a day. A page that cannot be
// fetched or carries no h-card is not an error: the item keeps the reference it
// already had, and the reader still knows there is an author.
func (r *cardResolver) Card(ctx context.Context, u string) (microsub.Card, bool) {
	if !isHTTPURL(u) {
		return microsub.Card{}, false
	}

	r.mu.Lock()
	cached, ok := r.cards[u]
	r.mu.Unlock()
	if ok && r.now().Sub(cached.at) < cardTTL {
		return cached.card, true
	}

	doc, err := readPage(ctx, r.fetcher, u)
	if err != nil {
		log.Printf("could not resolve author %s: %s\n", u, err)
		return microsub.Card{}, false
	}
	header, err := fetch.FeedHeader(ctx, r.fetcher, u, doc.contentType, bytes.NewBuffer(doc.body))
	if err != nil || header.Name == "" {
		return microsub.Card{}, false
	}
	card := microsub.Card{Type: "card", Name: header.Name, Photo: header.Photo, URL: u}

	r.mu.Lock()
	r.cards[u] = resolvedCard{card: card, at: r.now()}
	r.mu.Unlock()
	return card, true
}

// enrichItem runs the per-item enrichment steps and turns a panic in a parser
// into the untouched item. The markup is third-party input: a page that breaks
// the parser should cost that entry's enrichment, not the feed update and not
// the process.
func enrichItem(ctx context.Context, fetcher fetch.Fetcher, resolver *cardResolver, item microsub.Item, feedURL string) (out microsub.Item) {
	out = item
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered while enriching %s: %v\n%s", item.URL, r, debug.Stack())
		}
	}()
	out = fillFromPermalink(ctx, fetcher, item, feedURL)
	out = resolveAuthor(ctx, resolver, out)
	return out
}

// resolveAuthor fills in the name and photo of an author that is only a URL.
func resolveAuthor(ctx context.Context, resolver *cardResolver, item microsub.Item) microsub.Item {
	if item.Author == nil || item.Author.Name != "" || item.Author.URL == "" {
		return item
	}
	card, ok := resolver.Card(ctx, item.Author.URL)
	if !ok {
		return item
	}
	author := *item.Author
	if author.Name == "" {
		author.Name = card.Name
	}
	if author.Photo == "" {
		author.Photo = card.Photo
	}
	if author.Type == "" {
		author.Type = "card"
	}
	item.Author = &author
	return item
}

// hasContent reports whether an item already says something to read.
func hasContent(item microsub.Item) bool {
	if item.Content == nil {
		return false
	}
	return item.Content.Text != "" || item.Content.HTML != ""
}

// thin reports whether the feed gave nothing but a title: an entry that needs
// its permalink read before it is worth showing.
func thin(item microsub.Item, feedURL string) bool {
	if item.Name == "" || item.URL == "" || item.URL == feedURL {
		return false
	}
	if hasContent(item) || len(item.Photo) > 0 || item.Checkin != nil {
		return false
	}
	return true
}

// fillFromPermalink reads the entry's own page when the feed only listed a
// title. Feeds are allowed to be teasers; the entry is where the text is, and a
// reader that shows a headline and nothing else has simply not looked.
func fillFromPermalink(ctx context.Context, fetcher fetch.Fetcher, item microsub.Item, feedURL string) microsub.Item {
	if !thin(item, feedURL) {
		return item
	}
	doc, err := readPage(ctx, fetcher, item.URL)
	if err != nil {
		log.Printf("could not read %s: %s\n", item.URL, err)
		return item
	}
	entries, err := fetch.FeedItems(ctx, fetcher, item.URL, doc.contentType, bytes.NewBuffer(doc.body))
	if err != nil || len(entries) == 0 {
		return item
	}

	entry := entries[0]
	for _, candidate := range entries {
		if candidate.URL == item.URL || candidate.UID == item.URL {
			entry = candidate
			break
		}
	}

	if !hasContent(item) && hasContent(entry) {
		item.Content = entry.Content
	}
	if item.Author == nil {
		item.Author = entry.Author
	}
	if item.Summary == "" {
		item.Summary = entry.Summary
	}
	if len(item.Photo) == 0 {
		item.Photo = entry.Photo
	}
	if item.Name == "" {
		item.Name = entry.Name
	}
	item.Category = mergeCategories(item.Category, entry.Category)
	// The feed stays authoritative about when the entry was published: a
	// permalink read later must not move it in the timeline.
	return item
}

// mergeCategories adds categories the item does not already have.
func mergeCategories(have, add []string) []string {
	merged := append([]string{}, have...)
	seen := map[string]bool{}
	for _, c := range merged {
		seen[c] = true
	}
	for _, c := range add {
		if c == "" || seen[c] {
			continue
		}
		seen[c] = true
		merged = append(merged, c)
	}
	return merged
}
