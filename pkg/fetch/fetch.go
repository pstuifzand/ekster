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
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/pstuifzand/ekster/pkg/jf2"
	"github.com/pstuifzand/ekster/pkg/jsonfeed"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/rss"

	"willnorris.com/go/microformats"

	readability "github.com/go-shiori/go-readability"
)

// FeedHeader returns a new microsub.Feed with the information parsed from body.
func FeedHeader(fetcher Fetcher, fetchURL, contentType string, body io.Reader) (microsub.Feed, error) {
	log.Printf("ProcessContent %s\n", fetchURL)
	log.Println("Found " + contentType)

	feed := microsub.Feed{}

	u, _ := url.Parse(fetchURL)

	if strings.HasPrefix(contentType, "text/html") {
		data := microformats.Parse(body, u)
		author, ok := jf2.SimplifyMicroformatDataAuthor(data)
		if !ok {
			if strings.HasPrefix(author.URL, "http") {
				resp, err := fetcher.Fetch(author.URL)
				if err != nil {
					return feed, err
				}
				defer resp.Body.Close()
				u, _ := url.Parse(author.URL)

				md := microformats.Parse(resp.Body, u)

				author, ok = jf2.SimplifyMicroformatDataAuthor(md)
				if !ok {
					log.Println("Could not simplify the author")
				}
			}
		}

		feed.Type = "feed"
		feed.URL = fetchURL
		feed.Name = author.Name
		feed.Photo = author.Photo
	} else if strings.HasPrefix(contentType, "application/json") { // json feed?
		jfeed, err := jsonfeed.Parse(body)
		if err != nil {
			log.Printf("Error while parsing json feed: %s\n", err)
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
			log.Printf("Error while parsing rss/atom feed: %s\n", err)
			return feed, err
		}
		xfeed, err := rss.Parse(body)
		if err != nil {
			log.Printf("Error while parsing rss/atom feed: %s\n", err)
			return feed, err
		}

		feed.Type = "feed"
		feed.Name = xfeed.Title
		feed.URL = fetchURL
		feed.Description = xfeed.Description
		feed.Photo = xfeed.Image.URL
	} else {
		log.Printf("Unknown Content-Type: %s\n", contentType)
	}
	log.Println("Found feed: ", feed)
	return feed, nil
}

// FeedItems returns the items from the url, parsed from body.
func FeedItems(fetcher Fetcher, fetchURL, contentType string, body io.Reader) ([]microsub.Item, error) {
	log.Printf("ProcessContent %s\n", fetchURL)
	log.Println("Found " + contentType)

	items := []microsub.Item{}

	u, _ := url.Parse(fetchURL)

	if strings.HasPrefix(contentType, "text/html") {
		data := microformats.Parse(body, u)

		results := jf2.SimplifyMicroformatDataItems(data)

		// Filter items with "published" date
		for _, r := range results {
			if r.UID != "" {
				r.ID = hex.EncodeToString([]byte(r.UID))
			} else if r.URL != "" {
				r.ID = hex.EncodeToString([]byte(r.URL))
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
			item.ID = hex.EncodeToString([]byte(feedItem.ID))
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
				item.ID = hex.EncodeToString([]byte(feedItem.Link))
			} else {
				item.ID = hex.EncodeToString([]byte(feedItem.ID))
			}

			itemAuthor := &microsub.Card{}
			itemAuthor.Type = "card"
			itemAuthor.Name = feed.Title
			itemAuthor.URL = feed.Link
			itemAuthor.Photo = feed.Image.URL
			item.Author = itemAuthor

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
			mentions, err := parseContentMentions(fetcher, v.Content.HTML)
			if err != nil {
				log.Println("parseContentMentions", err)
				continue
			}

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

type mention struct {
	Href string
	Item microsub.Item
}

func parseContentMentions(fetcher Fetcher, s string) ([]mention, error) {
	node, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return nil, err
	}

	var mentions []mention

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		newMentions, err := parseContentMentionsRec(fetcher, c)
		if err != nil {
			log.Println("parseContentMentionsRec", err)
			continue
		}
		mentions = append(mentions, newMentions...)
	}

	return mentions, nil
}

// ErrNoMention is used when not mention was found
var ErrNoMention = errors.New("No mention")

func parseContentMentionProcessLink(fetcher Fetcher, node *html.Node) (mention, error) {
	href := getAttrPtr(node, "href")
	if href == nil {
		return mention{}, ErrNoMention
	}
	log.Println("Processing mentions:", *href)

	var hrefURL *url.URL
	var err error
	if hrefURL, err = url.Parse(*href); err != nil {
		return mention{}, err
	}

	resp, err := fetcher.Fetch(*href)
	if err != nil {
		return mention{}, err
	}
	defer resp.Body.Close()

	article, err := readability.FromReader(resp.Body, hrefURL)
	if err != nil {
		return mention{}, err
	}

	var item microsub.Item

	item.Type = "entry"
	item.Name = article.Title
	item.Content = &microsub.Content{
		Text: article.TextContent,
		HTML: article.Content,
	}
	if article.Image != "" {
		item.Photo = []string{article.Image}
	}

	item.Summary = article.Excerpt
	if article.Byline != "" {
		item.Author = &microsub.Card{
			Name: article.Byline,
		}
	}
	item.URL = *href

	return mention{
		Href: *href,
		Item: item,
	}, nil
}

func parseContentMentionsRec(fetcher Fetcher, node *html.Node) ([]mention, error) {
	var mentions []mention
	if isAtom(node, atom.A) {
		mention, err := parseContentMentionProcessLink(fetcher, node)
		if err != nil {
			log.Println("parseContentMentionProcessLink", err)
		} else {
			mentions = append(mentions, mention)
		}
	}

	for c := node.FirstChild; c != nil; c = c.NextSibling {
		newMentions, err := parseContentMentionsRec(fetcher, c)
		if err != nil {
			log.Println("parseContentMentionsRec", err)
			continue
		}
		mentions = append(mentions, newMentions...)
	}
	return mentions, nil
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
