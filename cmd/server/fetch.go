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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"rss"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/microsub-server/microsub"
	"willnorris.com/go/microformats"
)

type cacheItem struct {
	item    *microformats.Data
	created time.Time
}

var cache map[string]cacheItem

func init() {
	cache = make(map[string]cacheItem)
}

func (b *memoryBackend) ProcessContent(channel, fetchURL, contentType string, body io.Reader) error {
	u, _ := url.Parse(fetchURL)

	log.Println("Found " + contentType)
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
						return err
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
			r["_is_read"] = b.wasRead(channel, r)
			if r["_is_read"].(bool) {
				continue
			}
			if uid, e := r["uid"]; e {
				r["_id"] = hex.EncodeToString([]byte(uid.(string)))
			} else if uid, e := r["url"]; e {
				r["_id"] = hex.EncodeToString([]byte(uid.(string)))
			} else {
				r["_id"] = "" // generate random value
			}

			if _, e := r["published"]; e {
				item := mapToItem(r)
				b.channelAddItem(channel, item)
			}
		}
	} else if strings.HasPrefix(contentType, "application/json") { // json feed?
		var feed JSONFeed
		dec := json.NewDecoder(body)
		err := dec.Decode(&feed)
		if err != nil {
			log.Printf("Error while parsing json feed: %s\n", err)
			return err
		}
		for _, feedItem := range feed.Items {
			var item microsub.Item
			item.Name = feedItem.Title
			item.Content.HTML = feedItem.ContentHTML
			item.Content.Text = feedItem.ContentText
			item.URL = feedItem.URL
			item.Summary = []string{feedItem.Summary}
			item.Id = hex.EncodeToString([]byte(feedItem.ID))
			item.Published = feedItem.DatePublished
			b.channelAddItem(channel, item)
		}
	} else if strings.HasPrefix(contentType, "text/xml") || strings.HasPrefix(contentType, "application/rss+xml") || strings.HasPrefix(contentType, "application/atom+xml") {
		body, err := ioutil.ReadAll(body)
		if err != nil {
			log.Printf("Error while parsing rss/atom feed: %s\n", err)
			return err
		}
		feed, err := rss.Parse(body)
		if err != nil {
			log.Printf("Error while parsing rss/atom feed: %s\n", err)
			return err
		}

		for _, feedItem := range feed.Items {
			var item microsub.Item
			item.Name = feedItem.Title
			item.Content.HTML = feedItem.Summary
			item.Content.Text = feedItem.Content
			item.URL = feedItem.Link
			item.Id = hex.EncodeToString([]byte(feedItem.ID))
			item.Published = feedItem.Date.Format(time.RFC822Z)
			b.channelAddItem(channel, item)
		}

	} else {
		log.Printf("Unknown Content-Type: %s\n", contentType)
	}
	return nil
}

// Fetch3 fills stuff
func (b *memoryBackend) Fetch3(channel, fetchURL string) error {
	log.Printf("Fetching channel=%s fetchURL=%s\n", channel, fetchURL)

	resp, err := Fetch2(fetchURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return b.ProcessContent(channel, fetchURL, resp.Header.Get("Content-Type"), resp.Body)
}

func (b *memoryBackend) channelAddItem(channel string, item microsub.Item) {
	log.Printf("Adding item to channel %s\n", channel)
	log.Println(item)
	// send to redis
	channelKey := fmt.Sprintf("channel:%s:posts", channel)

	data, err := json.Marshal(item)
	if err != nil {
		log.Printf("error while creating item for redis: %v\n", err)
		return
	}

	forRedis := redisItem{
		Id:        item.Id,
		Published: item.Published,
		Read:      item.Read,
		Data:      data,
	}

	itemKey := fmt.Sprintf("item:%s", item.Id)
	_, err = redis.String(b.Redis.Do("HMSET", redis.Args{}.Add(itemKey).AddFlat(&forRedis)...))
	if err != nil {
		log.Printf("error while writing item for redis: %v\n", err)
		return
	}

	_, err = b.Redis.Do("SADD", channelKey, itemKey)
	if err != nil {
		log.Printf("error while adding item %s to channel %s for redis: %v\n", itemKey, channelKey, err)
		return
	}
}

type redisItem struct {
	Id        string
	Published string
	Read      bool
	Data      []byte
}

// Fetch2 fetches stuff
func Fetch2(fetchURL string) (*http.Response, error) {
	if !strings.HasPrefix(fetchURL, "http") {
		return nil, fmt.Errorf("error parsing %s as url", fetchURL)
	}

	u, err := url.Parse(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("error while fetching %s: %s", u, err)
	}

	return resp, err
}

func Fetch(fetchURL string) []microsub.Item {
	result := []microsub.Item{}

	if !strings.HasPrefix(fetchURL, "http") {
		return result
	}

	u, err := url.Parse(fetchURL)
	if err != nil {
		log.Printf("error parsing %s as url: %s", fetchURL, err)
		return result
	}
	resp, err := http.Get(u.String())
	if err != nil {
		log.Printf("error while fetching %s: %s", u, err)
		return result
	}

	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
		log.Printf("Content Type of %s = %s", fetchURL, resp.Header.Get("Content-Type"))
		return result
	}

	defer resp.Body.Close()
	data := microformats.Parse(resp.Body, u)
	jw := json.NewEncoder(os.Stdout)
	jw.SetIndent("", "    ")
	jw.Encode(data)

	author := microsub.Author{}

	for _, item := range data.Items {
		if item.Type[0] == "h-feed" {
			for _, child := range item.Children {
				previewItem := convertMfToItem(child)
				result = append(result, previewItem)
			}
		} else if item.Type[0] == "h-card" {
			mf := item
			author.Filled = true
			author.Type = "card"
			for prop, value := range mf.Properties {
				switch prop {
				case "url":
					author.URL = value[0].(string)
					break
				case "name":
					author.Name = value[0].(string)
					break
				case "photo":
					author.Photo = value[0].(string)
					break
				default:
					fmt.Printf("prop name not implemented for author: %s with value %#v\n", prop, value)
					break
				}
			}
		} else if item.Type[0] == "h-entry" {
			previewItem := convertMfToItem(item)
			result = append(result, previewItem)
		}
	}

	for i, item := range result {
		if !item.Author.Filled {
			result[i].Author = author
		}
	}

	return result
}

func convertMfToItem(mf *microformats.Microformat) microsub.Item {
	item := microsub.Item{}

	item.Type = mf.Type[0]

	for prop, value := range mf.Properties {
		switch prop {
		case "published":
			item.Published = value[0].(string)
			break
		case "url":
			item.URL = value[0].(string)
			break
		case "name":
			item.Name = value[0].(string)
			break
		case "latitude":
			item.Latitude = value[0].(string)
			break
		case "longitude":
			item.Longitude = value[0].(string)
			break
		case "like-of":
			for _, v := range value {
				item.LikeOf = append(item.LikeOf, v.(string))
			}
			break
		case "bookmark-of":
			for _, v := range value {
				item.BookmarkOf = append(item.BookmarkOf, v.(string))
			}
			break
		case "in-reply-to":
			for _, v := range value {
				item.InReplyTo = append(item.InReplyTo, v.(string))
			}
			break
		case "summary":
			if content, ok := value[0].(map[string]interface{}); ok {
				item.Content.HTML = content["html"].(string)
				item.Content.Text = content["value"].(string)
			} else if content, ok := value[0].(string); ok {
				item.Content.Text = content
			}
			break
		case "photo":
			for _, v := range value {
				item.Photo = append(item.Photo, v.(string))
			}
			break
		case "category":
			for _, v := range value {
				item.Category = append(item.Category, v.(string))
			}
			break
		case "content":
			if content, ok := value[0].(map[string]interface{}); ok {
				item.Content.HTML = content["html"].(string)
				item.Content.Text = content["value"].(string)
			} else if content, ok := value[0].(string); ok {
				item.Content.Text = content
			}
			break
		default:
			fmt.Printf("prop name not implemented: %s with value %#v\n", prop, value)
			break
		}
	}

	if item.Name == strings.TrimSpace(item.Content.Text) {
		item.Name = ""
	}

	// TODO: for like name is the field that is set
	if item.Content.HTML == "" && len(item.LikeOf) > 0 {
		item.Name = ""
	}

	fmt.Printf("%#v\n", item)
	return item
}
