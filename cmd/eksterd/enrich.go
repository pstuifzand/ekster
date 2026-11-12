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
	"net/url"
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
//
// It no longer reads the entry's own page. The feed is the source: an entry
// that carries only a title is a title, and fetching a page per entry turns one
// feed refresh into hundreds of requests at the sites the feed points at.
func enrichItem(ctx context.Context, fetcher fetch.Fetcher, resolver *cardResolver, item microsub.Item) (out microsub.Item) {
	out = item
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered while enriching %s: %v\n%s", item.URL, r, debug.Stack())
		}
	}()
	out = resolveAuthor(ctx, resolver, out)
	return out
}

// enrichReferenceItems fetches a small number of linked interaction targets when
// the incoming item did not already include their microformat data. This is
// deliberately separate from feed refresh: feed links are never fetched here.
func enrichReferenceItems(ctx context.Context, fetcher fetch.Fetcher, item microsub.Item) (out microsub.Item) {
	out = item
	defer func() {
		if recover() != nil {
			out = item
		}
	}()
	var targets []string
	for _, values := range [][]string{item.LikeOf, item.BookmarkOf, item.RepostOf, item.InReplyTo, item.MentionOf} {
		for _, target := range values {
			if target == "" {
				continue
			}
			if _, ok := item.Refs[target]; ok {
				continue
			}
			seen := false
			for _, existing := range targets {
				if existing == target {
					seen = true
					break
				}
			}
			if !seen {
				targets = append(targets, target)
			}
		}
	}
	if len(targets) > 5 {
		targets = targets[:5]
	}

	for _, target := range targets {
		parsed, err := url.Parse(target)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil {
			continue
		}
		doc, err := readPage(ctx, fetcher, target)
		if err != nil || !(strings.HasPrefix(strings.ToLower(doc.contentType), "text/html") || strings.HasPrefix(strings.ToLower(doc.contentType), "application/xhtml+xml")) {
			continue
		}
		items, err := fetch.FeedItems(ctx, fetcher, target, doc.contentType, bytes.NewReader(doc.body))
		if err != nil || len(items) == 0 {
			continue
		}
		ref := items[0]
		for _, candidate := range items {
			if candidate.URL == target || candidate.UID == target {
				ref = candidate
				break
			}
		}
		if ref.URL == "" {
			ref.URL = target
		}
		if item.Refs == nil {
			item.Refs = make(map[string]microsub.Item)
		}
		item.Refs[target] = ref
	}
	return item
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
