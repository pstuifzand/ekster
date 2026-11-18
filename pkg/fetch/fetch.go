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

// Package fetch provides an API for fetching information about urls.
package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"go.opentelemetry.io/otel/attribute"

	"github.com/pstuifzand/ekster/pkg/jf2"
	"github.com/pstuifzand/ekster/pkg/jsonfeed"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/observability"
	"github.com/pstuifzand/ekster/pkg/rss"

	"willnorris.com/go/microformats"
)

// htmlPageTitle returns the text content of an HTML document's title element.
func htmlPageTitle(page []byte) string {
	doc, err := html.Parse(bytes.NewReader(page))
	if err != nil {
		return ""
	}
	var findTitle func(*html.Node) string
	findTitle = func(n *html.Node) string {
		if n.Type == html.ElementNode && n.Data == "title" {
			var title strings.Builder
			var collect func(*html.Node)
			collect = func(node *html.Node) {
				if node.Type == html.TextNode {
					title.WriteString(node.Data)
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					collect(child)
				}
			}
			collect(n)
			return strings.TrimSpace(title.String())
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if title := findTitle(child); title != "" {
				return title
			}
		}
		return ""
	}
	return findTitle(doc)
}

// FeedHeader returns a new microsub.Feed with the information parsed from body.
func FeedHeader(ctx context.Context, fetcher Fetcher, fetchURL, contentType string, body io.Reader) (feed microsub.Feed, err error) {
	ctx, span := observability.Span(ctx, "feed.parse.header",
		attribute.String("feed.url", fetchURL),
		attribute.String("feed.content_type", contentType),
	)
	defer func() {
		span.SetAttributes(attribute.String("feed.name", feed.Name))
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	slog.DebugContext(ctx, "reading feed document", "url", fetchURL)
	slog.DebugContext(ctx, "feed content type", "content_type", contentType)

	u, _ := url.Parse(fetchURL)

	if strings.HasPrefix(contentType, "text/html") {
		page, err := ioutil.ReadAll(body)
		if err != nil {
			return feed, fmt.Errorf("could not read HTML page: %w", err)
		}
		data := microformats.Parse(bytes.NewReader(page), u)
		author, ok := jf2.SimplifyMicroformatDataAuthor(data)
		if !ok && author.URL != "" {
			resp, err := fetcher.FetchWithContext(ctx, author.URL)
			if err == nil {
				defer resp.Body.Close()
				authorURL, _ := url.Parse(author.URL)
				md := microformats.Parse(resp.Body, authorURL)
				if resolved, found := jf2.SimplifyMicroformatDataAuthor(md); found {
					author, ok = resolved, true
				}
			} else {
				slog.WarnContext(ctx, "could not fetch author page", "url", author.URL, "error", err)
			}
		}

		if !ok || author.Name == "" {
			if title := htmlPageTitle(page); title != "" {
				author.Name = title
			}
			if author.URL == "" {
				author.URL = fetchURL
			}
			author.Type = "card"
		}

		feed.Type = "feed"
		feed.URL = fetchURL
		feed.HomePage = fetchURL
		feed.Name = author.Name
		feed.Photo = author.Photo
		feed.Author = author
	} else if strings.HasPrefix(contentType, "application/json") { // json feed?
		jfeed, err := jsonfeed.Parse(body)
		if err != nil {
			slog.DebugContext(ctx, "could not parse json feed", "url", fetchURL, "error", err)
			return feed, err
		}

		feed.Type = "feed"
		feed.Name = jfeed.Title
		if feed.Name == "" {
			feed.Name = jfeed.Author.Name
		}

		feed.URL = jfeed.FeedURL

		if feed.URL == "" {
			feed.URL = fetchURL
		}
		feed.HomePage = jfeed.HomePageURL
		if feed.HomePage == "" {
			feed.HomePage = feed.URL
		}
		feed.Photo = jfeed.Icon

		if feed.Photo == "" {
			feed.Photo = jfeed.Author.Avatar
		}

		feed.Author.Type = "card"
		feed.Author.Name = jfeed.Author.Name
		feed.Author.URL = jfeed.Author.URL
		feed.Author.Photo = jfeed.Author.Avatar
	} else if strings.HasPrefix(contentType, "text/xml") || strings.HasPrefix(contentType, "application/rss+xml") || strings.HasPrefix(contentType, "application/atom+xml") || strings.HasPrefix(contentType, "application/xml") {
		body, err := ioutil.ReadAll(body)
		if err != nil {
			slog.DebugContext(ctx, "could not parse rss/atom feed", "content_type", contentType, "error", err)
			return feed, err
		}
		xfeed, err := rss.Parse(body)
		if err != nil {
			slog.DebugContext(ctx, "could not parse rss/atom feed", "content_type", contentType, "error", err)
			return feed, err
		}

		feed.Type = "feed"
		feed.Name = xfeed.Title
		feed.URL = fetchURL
		feed.HomePage = xfeed.Link
		if feed.HomePage == "" {
			feed.HomePage = fetchURL
		}
		feed.Description = xfeed.Description
		feed.Photo = xfeed.Image.URL
	} else {
		slog.WarnContext(ctx, "unknown content type", "content_type", contentType)
	}
	slog.DebugContext(ctx, "found feed", "url", fetchURL, "feed", fmt.Sprint(feed))
	return feed, nil
}

// FeedItems returns the items from the url, parsed from body.
func FeedItems(ctx context.Context, fetcher Fetcher, fetchURL, contentType string, body io.Reader) (items []microsub.Item, err error) {
	ctx, span := observability.Span(ctx, "feed.parse.items",
		attribute.String("feed.url", fetchURL),
		attribute.String("feed.content_type", contentType),
		attribute.String("feed.format", Format(contentType)),
	)
	defer func() {
		span.SetAttributes(attribute.Int("feed.items", len(items)))
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	slog.DebugContext(ctx, "reading feed document", "url", fetchURL)
	slog.DebugContext(ctx, "feed content type", "content_type", contentType)

	items = []microsub.Item{}

	u, _ := url.Parse(fetchURL)

	if strings.HasPrefix(contentType, "text/html") {
		data := microformats.Parse(body, u)

		results := jf2.SimplifyMicroformatDataItems(data)

		// Filter items with "published" date
		for _, r := range results {
			if r.UID != "" {
				r.ID = itemUID(r.UID)
			} else if r.URL != "" {
				r.ID = itemUID(r.URL)
			} else {
				continue
			}

			items = append(items, r)
		}
	} else if strings.HasPrefix(contentType, "application/json") && strings.HasPrefix(contentType, "application/feed+json") { // json feed?
		var feed jsonfeed.Feed
		err := json.NewDecoder(body).Decode(&feed)
		if err != nil {
			return items, fmt.Errorf("could not parse as jsonfeed: %v", err)
		}

		author := &microsub.Card{}
		author.Type = "card"
		author.Name = feed.Author.Name
		author.URL = feed.Author.URL
		author.Photo = feed.Author.Avatar

		if author.Photo == "" {
			author.Photo = feed.Icon
		}

		for _, feedItem := range feed.Items {
			var item microsub.Item
			item.Type = "entry"
			item.Name = feedItem.Title
			item.Content = &microsub.Content{}
			item.Content.HTML = feedItem.ContentHTML
			item.Content.Text = feedItem.ContentText
			item.URL = feedItem.URL
			item.ID = itemUID(feedItem.ID)
			item.Published = feedItem.DatePublished

			itemAuthor := &microsub.Card{}
			itemAuthor.Type = "card"
			itemAuthor.Name = feedItem.Author.Name
			itemAuthor.URL = feedItem.Author.URL
			itemAuthor.Photo = feedItem.Author.Avatar
			if itemAuthor.URL != "" {
				item.Author = itemAuthor
			} else {
				item.Author = author
			}
			item.Photo = []string{feedItem.Image}
			items = append(items, item)
		}
	} else if strings.HasPrefix(contentType, "text/xml") || strings.HasPrefix(contentType, "application/rss+xml") || strings.HasPrefix(contentType, "application/atom+xml") || strings.HasPrefix(contentType, "application/xml") {
		body, err := ioutil.ReadAll(body)
		if err != nil {
			return items, fmt.Errorf("could not read feed for rss/atom: %v", err)
		}
		feed, err := rss.Parse(body)
		if err != nil {
			return items, fmt.Errorf("while parsing rss/atom feed: %v", err)
		}

		baseURL, _ := url.Parse(fetchURL)

		for _, feedItem := range feed.Items {
			var item microsub.Item
			item.Type = "entry"
			item.Name = feedItem.Title
			item.Content = &microsub.Content{}
			if len(feedItem.Content) > 0 {
				item.Content.HTML = expandHref(feedItem.Content, baseURL)
			}
			if len(feedItem.Summary) > 0 {
				if len(item.Content.HTML) == 0 {
					item.Content.HTML = feedItem.Summary
				}
			}
			item.URL = feedItem.Link
			if feedItem.ID == "" {
				item.ID = itemUID(feedItem.Link)
			} else {
				item.ID = itemUID(feedItem.ID)
			}

			if name := strings.TrimSpace(feedItem.Author); name != "" {
				// The entry names its own author (dc:creator, atom person —
				// hnrss, WordPress, most news feeds). The feed's link and icon
				// belong to the publication, not to this person, so the card
				// carries the name alone and the reader falls back to initials.
				item.Author = &microsub.Card{Type: "card", Name: name}
			}
			// A feed that names nobody leaves the entry without an author. It
			// used to be filled with the feed's identity, which put a
			// publication into a person-shaped field ("Bullet Journal - The
			// Bullet Journalist" on every author line). Which feed carried the
			// entry is already stated once, in item.Source; a reader shows that
			// when there is no byline, and shows both when there is one.

			item.Published = feedItem.Date.Format(time.RFC3339)
			items = append(items, item)
		}
	} else {
		return items, fmt.Errorf("unknown content-type %s for url %s", contentType, fetchURL)
	}

	for i, v := range items {
		// Clear type of author, when other fields also aren't set
		if v.Author != nil && v.Author.Name == "" && v.Author.Photo == "" && v.Author.URL == "" {
			v.Author = nil
			items[i] = v
		}
	}

	for i, v := range items {
		// Process mentions inside the content
		if v.Content != nil && v.Content.HTML != "" {
			mentions := parseContentMentions(v.Content.HTML)

			if v.Refs == nil {
				v.Refs = make(map[string]microsub.Item)
			}

			for _, m := range mentions {
				v.Refs[m.Href] = m.Item
				v.MentionOf = append(v.MentionOf, m.Href)
			}

			items[i] = v
		}
	}

	return items, nil
}

// uidMaxLen is the width of items."uid" (varchar(512)). An id wider than this
// fails the insert and the entry is dropped with a driver error in the log.
const uidMaxLen = 512

// itemUID is the opaque identity ekster stores for a feed entry.
//
// It is the hex of the entry's own id (or link) because that is what every
// stored item already uses, and changing the scheme would make each entry
// arrive as a new one. Hex doubles the length though, so a source id longer
// than 256 characters — a bookmark URL with a long query string, which is
// exactly what a Pinboard feed carries — produced a value Postgres refused
// and a silently missing item. Hash what does not fit: the uid is opaque, so
// a digest dedupes a re-fetch just as well, and the "sha256:" prefix keeps it
// from colliding with the hex of some 32-character id.
func itemUID(value string) string {
	encoded := hex.EncodeToString([]byte(value))
	if len(encoded) <= uidMaxLen {
		return encoded
	}
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

type mention struct {
	Href string
	Item microsub.Item
}

// HostName is the publisher identity of a site url: the bare host, without
// "www.". A host reads as a publication ("bulletjournal.com") where a feed
// title reads as a channel name ("Bullet Journal - The Bullet Journalist"),
// which is why an item's source is named this way.
func HostName(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// Format names the document format for a given content type. It is used for
// trace attributes, so a span shows at a glance which parser ran.
func Format(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "text/html"):
		return "jf2"
	case strings.HasPrefix(contentType, "application/feed+json"),
		strings.HasPrefix(contentType, "application/json"):
		return "jsonfeed"
	case strings.HasPrefix(contentType, "text/xml"),
		strings.HasPrefix(contentType, "application/rss+xml"),
		strings.HasPrefix(contentType, "application/atom+xml"),
		strings.HasPrefix(contentType, "application/xml"):
		return "rss/atom"
	}
	return "unknown"
}

// parseContentMentions lists the links an entry points at, without fetching
// them.
//
// It used to fetch every anchor and run readability on the response, keeping the
// first one that parsed as an article. A release feed with forty entries and
// fifteen links each turned one refresh into six hundred requests at the sites
// the feed named, most of which were not mentions at all. The link is what the
// markup actually says; the article behind it is the reader's job to fetch.
func parseContentMentions(s string) []mention {
	node, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return nil
	}

	var mentions []mention
	seen := map[string]bool{}

	walkLinks(node, func(href string) {
		if href == "" || seen[href] {
			return
		}
		seen[href] = true
		mentions = append(mentions, mention{
			Href: href,
			Item: microsub.Item{
				Type: "entry",
				URL:  href,
			},
		})
	})

	return mentions
}

func walkLinks(node *html.Node, fn func(href string)) {
	if isAtom(node, atom.A) {
		if href := getAttrPtr(node, "href"); href != nil {
			fn(*href)
		}
	}
	for c := node.FirstChild; c != nil; c = c.NextSibling {
		walkLinks(c, fn)
	}
}

// expandHref expands relative URLs in a.href and img.src attributes to be absolute URLs.
func expandHref(s string, base *url.URL) string {
	var buf bytes.Buffer

	node, _ := html.Parse(strings.NewReader(s))

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		expandHrefRec(c, base)
	}

	html.Render(&buf, node)

	return buf.String()
}

func getAttrPtr(node *html.Node, name string) *string {
	if node == nil {
		return nil
	}
	for i, attr := range node.Attr {
		if strings.EqualFold(attr.Key, name) {
			return &node.Attr[i].Val
		}
	}
	return nil
}

func isAtom(node *html.Node, atoms ...atom.Atom) bool {
	if node == nil {
		return false
	}
	for _, atom := range atoms {
		if atom == node.DataAtom {
			return true
		}
	}
	return false
}

func expandHrefRec(node *html.Node, base *url.URL) {
	if isAtom(node, atom.A) {
		href := getAttrPtr(node, "href")
		if href != nil {
			if urlParsed, err := url.Parse(*href); err == nil {
				urlParsed = base.ResolveReference(urlParsed)
				*href = urlParsed.String()
			}
		}
		return
	}

	if isAtom(node, atom.Img) {
		href := getAttrPtr(node, "src")
		if href != nil {
			if urlParsed, err := url.Parse(*href); err == nil {
				urlParsed = base.ResolveReference(urlParsed)
				*href = urlParsed.String()
			}
		}
		return
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		expandHrefRec(c, base)
	}
}
