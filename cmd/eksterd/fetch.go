// fetch url in different ways
/*
   Microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"rss"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"willnorris.com/go/microformats"
)

type cacheItem struct {
	item    []byte
	created time.Time
}

var cache map[string]cacheItem

func init() {
	cache = make(map[string]cacheItem)
}

func (b *memoryBackend) feedHeader(fetchURL, contentType string, body io.Reader) (microsub.Feed, error) {
	log.Printf("ProcessContent %s\n", fetchURL)
	log.Println("Found " + contentType)

	feed := microsub.Feed{}

	u, _ := url.Parse(fetchURL)

	var card interface{}

	if strings.HasPrefix(contentType, "text/html") {
		data := microformats.Parse(body, u)
		results := simplifyMicroformatData(data)
		found := -1
		for i, r := range results {
			if r["type"] == "card" {
				found = i
				break
			}
		}

		if found >= 0 {
			card = results[found]

			if as, ok := card.(string); ok {
				if strings.HasPrefix(as, "http") {
					resp, err := Fetch2(fetchURL)
					if err != nil {
						return feed, err
					}
					defer resp.Body.Close()
					u, _ := url.Parse(fetchURL)

					md := microformats.Parse(resp.Body, u)
					author := simplifyMicroformatData(md)
					for _, a := range author {
						if a["type"] == "card" {
							card = a
							break
						}
					}
				}
			}

			// use object
		}

		feed.Type = "feed"
		feed.URL = fetchURL
		if cardMap, ok := card.(map[string]interface{}); ok {
			if name, ok := cardMap["name"].(string); ok {
				feed.Name = name
			}
			if name, ok := cardMap["photo"].(string); ok {
				feed.Photo = name
			} else if name, ok := cardMap["photo"].([]interface{}); ok {
				feed.Photo = name[0].(string)
			}
		}
	} else if strings.HasPrefix(contentType, "application/json") { // json feed?
		var jfeed JSONFeed
		dec := json.NewDecoder(body)
		err := dec.Decode(&jfeed)
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

func (b *memoryBackend) feedItems(fetchURL, contentType string, body io.Reader) ([]microsub.Item, error) {
	log.Printf("ProcessContent %s\n", fetchURL)
	log.Println("Found " + contentType)

	items := []microsub.Item{}

	u, _ := url.Parse(fetchURL)

	if strings.HasPrefix(contentType, "text/html") {
		data := microformats.Parse(body, u)
		results := simplifyMicroformatData(data)
		found := -1
		for {
			for i, r := range results {
				if r["type"] == "card" {
					found = i
					break
				}
			}
			if found >= 0 {
				card := results[found]
				results = append(results[:found], results[found+1:]...)
				for i := range results {
					if results[i]["type"] == "entry" && results[i]["author"] == card["url"] {
						results[i]["author"] = card
					}
				}
				found = -1
				continue
			}
			break
		}

		for i, r := range results {
			if as, ok := r["author"].(string); ok {
				if r["type"] == "entry" && strings.HasPrefix(as, "http") {
					resp, err := Fetch2(fetchURL)
					if err != nil {
						return items, err
					}
					defer resp.Body.Close()
					u, _ := url.Parse(fetchURL)

					md := microformats.Parse(resp.Body, u)
					author := simplifyMicroformatData(md)
					for _, a := range author {
						if a["type"] == "card" {
							results[i]["author"] = a
							break
						}
					}
				}
			}
		}

		// Filter items with "published" date
		for _, r := range results {
			if uid, e := r["uid"]; e {
				r["_id"] = hex.EncodeToString([]byte(uid.(string)))
			} else if uid, e := r["url"]; e {
				r["_id"] = hex.EncodeToString([]byte(uid.(string)))
			} else {
				continue
				//r["_id"] = "" // generate random value
			}

			// mapToItem adds published
			item := mapToItem(r)
			items = append(items, item)
		}
	} else if strings.HasPrefix(contentType, "application/json") { // json feed?
		var feed JSONFeed
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
			item.Summary = []string{feedItem.Summary}
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

		for _, feedItem := range feed.Items {
			var item microsub.Item
			item.Type = "entry"
			item.Name = feedItem.Title
			item.Content = &microsub.Content{}
			if len(feedItem.Content) > 0 {
				item.Content.HTML = feedItem.Content
			} else if len(feedItem.Summary) > 0 {
				item.Content.HTML = feedItem.Summary
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
			v.Type = ""
			items[i] = v
		}
	}

	for _, item := range items {
		log.Printf("ID=%s Name=%s\n", item.ID, item.Name)
		log.Printf("Author=%#v\n", item.Author)
		if item.Content != nil {
			log.Printf("Text=%s\n", item.Content.Text)
			log.Printf("HTML=%s\n", item.Content.HTML)
		}
	}

	return items, nil
}

func (b *memoryBackend) ProcessContent(channel, fetchURL, contentType string, body io.Reader) error {
	conn := pool.Get()
	defer conn.Close()

	items, err := b.feedItems(fetchURL, contentType, body)
	if err != nil {
		return err
	}

	for _, item := range items {
		item.Read = false
		err = b.channelAddItem(conn, channel, item)
		if err != nil {
			log.Printf("ERROR: %s\n", err)
		}
	}

	err = b.updateChannelUnreadCount(conn, channel)
	if err != nil {
		return err
	}

	return nil
}

// Fetch3 fills stuff
func (b *memoryBackend) Fetch3(channel, fetchURL string) (*http.Response, error) {
	log.Printf("Fetching channel=%s fetchURL=%s\n", channel, fetchURL)
	return Fetch2(fetchURL)
}

func (b *memoryBackend) channelAddItem(conn redis.Conn, channel string, item microsub.Item) error {
	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)

	if item.Published == "" {
		item.Published = time.Now().Format(time.RFC3339)
	}

	data, err := json.Marshal(item)
	if err != nil {
		log.Printf("error while creating item for redis: %v\n", err)
		return err
	}

	forRedis := redisItem{
		ID:        item.ID,
		Published: item.Published,
		Read:      item.Read,
		Data:      data,
	}

	itemKey := fmt.Sprintf("item:%s", item.ID)
	_, err = redis.String(conn.Do("HMSET", redis.Args{}.Add(itemKey).AddFlat(&forRedis)...))
	if err != nil {
		return fmt.Errorf("error while writing item for redis: %v", err)
	}

	readChannelKey := fmt.Sprintf("channel:%s:read", channel)
	isRead, err := redis.Bool(conn.Do("SISMEMBER", readChannelKey, itemKey))
	if err != nil {
		return err
	}

	if isRead {
		return nil
	}

	score, err := time.Parse(time.RFC3339, item.Published)
	if err != nil {
		return fmt.Errorf("error can't parse %s as time", item.Published)
	}

	_, err = redis.Int64(conn.Do("ZADD", zchannelKey, score.Unix()*1.0, itemKey))
	if err != nil {
		return fmt.Errorf("error while zadding item %s to channel %s for redis: %v", itemKey, zchannelKey, err)
	}

	return nil
}

func (b *memoryBackend) updateChannelUnreadCount(conn redis.Conn, channel string) error {
	if c, e := b.Channels[channel]; e {
		zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)
		unread, err := redis.Int(conn.Do("ZCARD", zchannelKey))
		if err != nil {
			return fmt.Errorf("error: while updating channel unread count for %s: %s", channel, err)
		}
		c.Unread = unread
		b.Channels[channel] = c
	}
	return nil
}

type redisItem struct {
	ID        string
	Published string
	Read      bool
	Data      []byte
}

// Fetch2 fetches stuff
func Fetch2(fetchURL string) (*http.Response, error) {
	conn := pool.Get()
	defer conn.Close()

	if !strings.HasPrefix(fetchURL, "http") {
		return nil, fmt.Errorf("error parsing %s as url, has no http(s) prefix", fetchURL)
	}

	u, err := url.Parse(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
	}

	req, err := http.NewRequest("GET", u.String(), nil)

	cacheKey := fmt.Sprintf("http_cache:%s", u.String())
	data, err := redis.Bytes(conn.Do("GET", cacheKey))
	if err == nil {
		log.Printf("HIT %s\n", u.String())
		rd := bufio.NewReader(bytes.NewReader(data))
		return http.ReadResponse(rd, req)
	}

	log.Printf("MISS %s\n", u.String())

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error while fetching %s: %s", u, err)
	}

	var b bytes.Buffer
	resp.Write(&b)

	cachedCopy := make([]byte, b.Len())
	cur := b.Bytes()
	copy(cachedCopy, cur)

	conn.Do("SET", cacheKey, cachedCopy, "EX", 60*60)

	cachedResp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(cachedCopy)), req)
	return cachedResp, err
}
