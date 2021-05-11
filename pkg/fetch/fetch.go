// Package fetch provides an API for fetching information about urls.
package fetch

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"p83.nl/go/ekster/pkg/jf2"
	"p83.nl/go/ekster/pkg/jsonfeed"
	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/rss"

	"willnorris.com/go/microformats"
)

// FeedHeader returns a new microsub.Feed with the information parsed from body.
func FeedHeader(fetcher FetcherFunc, fetchURL, contentType string, body io.Reader) (microsub.Feed, error) {
	log.Printf("ProcessContent %s\n", fetchURL)
	log.Println("Found " + contentType)

	feed := microsub.Feed{}

	u, _ := url.Parse(fetchURL)

	if strings.HasPrefix(contentType, "text/html") {
		data := microformats.Parse(body, u)
		author, ok := jf2.SimplifyMicroformatDataAuthor(data)
		if !ok {
			if strings.HasPrefix(author.URL, "http") {
				resp, err := fetcher(author.URL)
				if err != nil {
					return feed, err
				}
				defer resp.Body.Close()
				u, _ := url.Parse(author.URL)

				md := microformats.Parse(resp.Body, u)

				author, ok = jf2.SimplifyMicroformatDataAuthor(md)
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
func FeedItems(fetcher FetcherFunc, fetchURL, contentType string, body io.Reader) ([]microsub.Item, error) {
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
	} else if strings.HasPrefix(contentType, "application/json") { // json feed?
		var feed jsonfeed.Feed
		dec := json.NewDecoder(body)
		err := dec.Decode(&feed)
		if err != nil {
			log.Printf("Error while parsing json feed: %s\n", err)
			return items, err
		}

		log.Printf("%#v\n", feed)

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
			log.Printf("Error while parsing rss/atom feed: %s\n", err)
			return items, err
		}
		feed, err := rss.Parse(body)
		if err != nil {
			log.Printf("Error while parsing rss/atom feed: %s\n", err)
			return items, err
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
		log.Printf("Unknown Content-Type: %s\n", contentType)
	}

	for i, v := range items {
		// Clear type of author, when other fields also aren't set
		if v.Author != nil && v.Author.Name == "" && v.Author.Photo == "" && v.Author.URL == "" {
			v.Author = nil
			items[i] = v
		}
	}

	return items, nil
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
