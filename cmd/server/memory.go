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
	"log"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/microsub-server/microsub"
	"willnorris.com/go/microformats"
)

type memoryBackend struct {
	Redis    redis.Conn
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	NextUid  int

	ticker *time.Ticker
	quit   chan struct{}
}

type Debug interface {
	Debug()
}

func (b *memoryBackend) Debug() {
	fmt.Println(b.Channels)
}

func (b *memoryBackend) load() {
	filename := "backend.json"
	f, err := os.Open(filename)
	if err != nil {
		panic("cant open backend.json")
	}
	defer f.Close()
	jw := json.NewDecoder(f)
	err = jw.Decode(b)
	if err != nil {
		panic("cant open backend.json")
	}

	for uid, channel := range b.Channels {
		log.Printf("loading channel %s - %s\n", uid, channel.Name)
		for _, feed := range b.Feeds[uid] {
			log.Printf("- loading feed %s\n", feed.URL)
			b.Fetch3(uid, feed.URL)
		}
	}
}

func (b *memoryBackend) save() {
	filename := "backend.json"
	f, _ := os.Create(filename)
	defer f.Close()
	jw := json.NewEncoder(f)
	jw.Encode(b)
}

func loadMemoryBackend(conn redis.Conn) microsub.Microsub {
	backend := &memoryBackend{}
	backend.Redis = conn
	backend.load()

	return backend
}

func createMemoryBackend() microsub.Microsub {
	backend := memoryBackend{}
	defer backend.save()
	backend.Channels = make(map[string]microsub.Channel)
	backend.Feeds = make(map[string][]microsub.Feed)
	channels := []microsub.Channel{
		microsub.Channel{UID: "0000", Name: "default"},
		microsub.Channel{UID: "0001", Name: "notifications"},
		microsub.Channel{UID: "1000", Name: "Friends"},
		microsub.Channel{UID: "1001", Name: "Family"},
	}
	for _, c := range channels {
		backend.Channels[c.UID] = c
	}
	backend.NextUid = 1002
	return &backend
}

// ChannelsGetList gets no channels
func (b *memoryBackend) ChannelsGetList() []microsub.Channel {
	channels := []microsub.Channel{}
	for _, v := range b.Channels {
		channels = append(channels, v)
	}
	return channels
}

// ChannelsCreate creates no channels
func (b *memoryBackend) ChannelsCreate(name string) microsub.Channel {
	defer b.save()
	uid := fmt.Sprintf("%04d", b.NextUid)
	channel := microsub.Channel{
		UID:  uid,
		Name: name,
	}

	b.Channels[channel.UID] = channel
	b.Feeds[channel.UID] = []microsub.Feed{}
	b.NextUid++
	return channel
}

// ChannelsUpdate updates no channels
func (b *memoryBackend) ChannelsUpdate(uid, name string) microsub.Channel {
	defer b.save()
	if c, e := b.Channels[uid]; e {
		c.Name = name
		b.Channels[uid] = c
		return c
	}
	return microsub.Channel{}
}

func (b *memoryBackend) ChannelsDelete(uid string) {
	defer b.save()
	if _, e := b.Channels[uid]; e {
		delete(b.Channels, uid)
	}
}

func mapToAuthor(result map[string]interface{}) microsub.Author {
	item := microsub.Author{}
	item.Type = "card"
	if name, e := result["name"]; e {
		item.Name = name.(string)
	}
	if u, e := result["url"]; e {
		item.URL = u.(string)
	}
	if photo, e := result["photo"]; e {
		item.Photo = photo.(string)
	}
	return item
}

func mapToItem(result map[string]interface{}) microsub.Item {
	item := microsub.Item{}

	item.Type = "entry"

	if name, e := result["name"]; e {
		item.Name = name.(string)
	}

	if url, e := result["url"]; e {
		item.URL = url.(string)
	}

	if uid, e := result["uid"]; e {
		item.UID = uid.(string)
	}

	if author, e := result["author"]; e {
		item.Author = mapToAuthor(author.(map[string]interface{}))
	}

	if content, e := result["content"]; e {
		if c, ok := content.(map[string]interface{}); ok {
			if html, e2 := c["html"]; e2 {
				item.Content.HTML = html.(string)
			}
			if text, e2 := c["value"]; e2 {
				item.Content.Text = text.(string)
			}
		}
	}

	// TODO: Check how to improve this

	// if value, e := result["like-of"]; e {
	// 	for _, v := range value.([]interface{}) {
	// 		item.LikeOf = append(item.LikeOf, v.(string))
	// 	}
	// }

	// if value, e := result["repost-of"]; e {
	// 	for _, v := range value.([]interface{}) {
	// 		item.RepostOf = append(item.RepostOf, v.(string))
	// 	}
	// }

	// if value, e := result["bookmark-of"]; e {
	// 	for _, v := range value.([]interface{}) {
	// 		item.BookmarkOf = append(item.BookmarkOf, v.(string))
	// 	}
	// }

	// if value, e := result["in-reply-to"]; e {
	// 	for _, v := range value.([]interface{}) {
	// 		if replyTo, ok := v.(string); ok {
	// 			item.InReplyTo = append(item.InReplyTo, replyTo)
	// 		} else if cite, ok := v.(map[string]interface{}); ok {
	// 			item.InReplyTo = append(item.InReplyTo, cite["url"].(string))
	// 		}
	// 	}
	// }

	// if value, e := result["photo"]; e {
	// 	for _, v := range value.([]interface{}) {
	// 		item.Photo = append(item.Photo, v.(string))
	// 	}
	// }

	// if value, e := result["category"]; e {

	// 	if cats, ok := value.([]string); ok {
	// 		for _, v := range cats {
	// 			item.Category = append(item.Category, v)
	// 		}
	// 	} else {
	// 		item.Category = append(item.Category, value.(string))
	// 	}
	// }

	if published, e := result["published"]; e {
		item.Published = published.(string)
	}

	if updated, e := result["updated"]; e {
		item.Updated = updated.(string)
	}

	if id, e := result["_id"]; e {
		item.Id = id.(string)
	}
	if read, e := result["_is_read"]; e {
		item.Read = read.(bool)
	}

	return item
}

func (b *memoryBackend) run() {
	b.ticker = time.NewTicker(10 * time.Minute)
	b.quit = make(chan struct{})

	go func() {
		for {
			select {
			case <-b.ticker.C:
				for uid, _ := range b.Channels {
					for _, feed := range b.Feeds[uid] {
						b.Fetch3(uid, feed.URL)
					}
				}
			case <-b.quit:
				b.ticker.Stop()
				return
			}
		}
	}()
}

func (b *memoryBackend) TimelineGet(after, before, channel string) microsub.Timeline {
	log.Printf("TimelineGet %s\n", channel)
	feeds := b.FollowGetList(channel)
	log.Println(feeds)

	items := []microsub.Item{}

	channelKey := fmt.Sprintf("channel:%s:posts", channel)

	itemJsons, err := redis.ByteSlices(b.Redis.Do("SORT", channelKey, "BY", "*->Published", "GET", "*->Data", "ASC", "ALPHA"))
	if err != nil {
		log.Println(err)
		return microsub.Timeline{
			Paging: microsub.Pagination{},
			Items:  items,
		}
	}

	for _, obj := range itemJsons {
		item := microsub.Item{}
		json.Unmarshal(obj, &item)
		items = append(items, item)
	}

	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  items,
	}
}

//panic if s is not a slice
func reverseSlice(s interface{}) {
	size := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, size-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}

func (b *memoryBackend) checkRead(channel string, uid string) bool {
	args := redis.Args{}.Add(fmt.Sprintf("timeline:%s:read", channel)).Add("item:" + uid)
	member, err := redis.Bool(b.Redis.Do("SISMEMBER", args...))
	if err != nil {
		log.Printf("Checking read for channel %s item %s has failed\n", channel, uid)
	}
	return member
}

func (b *memoryBackend) wasRead(channel string, item map[string]interface{}) bool {
	if uid, e := item["uid"]; e {
		uid = hex.EncodeToString([]byte(uid.(string)))
		return b.checkRead(channel, uid.(string))
	}

	if uid, e := item["url"]; e {
		uid = hex.EncodeToString([]byte(uid.(string)))
		return b.checkRead(channel, uid.(string))
	}

	return false
}

func (b *memoryBackend) FollowGetList(uid string) []microsub.Feed {
	return b.Feeds[uid]
}

func (b *memoryBackend) FollowURL(uid string, url string) microsub.Feed {
	defer b.save()
	feed := microsub.Feed{Type: "feed", URL: url}
	b.Feeds[uid] = append(b.Feeds[uid], feed)
	return feed
}

func (b *memoryBackend) UnfollowURL(uid string, url string) {
	defer b.save()
	index := -1
	for i, f := range b.Feeds[uid] {
		if f.URL == url {
			index = i
			break
		}
	}
	if index >= 0 {
		feeds := b.Feeds[uid]
		b.Feeds[uid] = append(feeds[:index], feeds[index+1:]...)
	}
}

func checkURL(u string) bool {
	testURL, err := url.Parse(u)
	if err != nil {
		return false
	}

	resp, err := http.Head(testURL.String())

	if err != nil {
		log.Printf("Error while HEAD %s: %v\n", u, err)
		return false
	}

	defer resp.Body.Close()

	return resp.StatusCode == 200
}

func getPossibleURLs(query string) []string {
	urls := []string{}
	if !(strings.HasPrefix(query, "https://") || strings.HasPrefix(query, "http://")) {
		secureURL := "https://" + query
		if checkURL(secureURL) {
			urls = append(urls, secureURL)
		} else {
			unsecureURL := "http://" + query
			if checkURL(unsecureURL) {
				urls = append(urls, unsecureURL)
			}
		}
	} else {
		urls = append(urls, query)
	}
	return urls
}

func (b *memoryBackend) Search(query string) []microsub.Feed {
	urls := getPossibleURLs(query)

	feeds := []microsub.Feed{}

	for _, u := range urls {
		resp, err := Fetch2(u)
		if err != nil {
			log.Printf("Error while fetching %s: %v\n", u, err)
			continue
		}
		fetchUrl, err := url.Parse(u)
		md := microformats.Parse(resp.Body, fetchUrl)
		if err != nil {
			log.Printf("Error while fetching %s: %v\n", u, err)
			continue
		}

		feeds = append(feeds, microsub.Feed{Type: "feed", URL: u})

		if alts, e := md.Rels["alternate"]; e {
			for _, alt := range alts {
				relURL := md.RelURLs[alt]
				log.Printf("alternate found with type %s %#v\n", relURL.Type, relURL)
				if relURL.Type == "application/rss+xml" {
					feeds = append(feeds, microsub.Feed{Type: "feed", URL: alt})
				} else if relURL.Type == "application/atom+xml" {
					feeds = append(feeds, microsub.Feed{Type: "feed", URL: alt})
				} else if relURL.Type == "application/json" {
					feeds = append(feeds, microsub.Feed{Type: "feed", URL: alt})
				}
			}
		}
	}

	return feeds
}

func (b *memoryBackend) PreviewURL(previewURL string) microsub.Timeline {
	resp, err := Fetch2(previewURL)
	if err != nil {
		log.Printf("Error while fetching %s: %v\n", previewURL, err)
		return microsub.Timeline{}
	}
	fetchUrl, err := url.Parse(previewURL)
	md := microformats.Parse(resp.Body, fetchUrl)
	if err != nil {
		log.Printf("Error while fetching %s: %v\n", previewURL, err)
		return microsub.Timeline{}
	}
	if err != nil {
		log.Println(err)
		return microsub.Timeline{}
	}
	results := simplifyMicroformatData(md)
	log.Println(results)
	items := []microsub.Item{}
	for _, r := range results {
		item := mapToItem(r)
		items = append(items, item)
	}
	return microsub.Timeline{
		Items: items,
	}
}

func (b *memoryBackend) MarkRead(channel string, uids []string) {
	log.Printf("Marking read for %s %v\n", channel, uids)

	itemUIDs := []string{}
	for _, uid := range uids {
		itemUIDs = append(itemUIDs, "item:"+uid)
	}

	args := redis.Args{}.Add(fmt.Sprintf("timeline:%s:read", channel)).AddFlat(itemUIDs)
	if _, err := b.Redis.Do("SADD", args...); err != nil {
		log.Printf("Marking read for channel %s has failed\n", channel)
	}
	log.Printf("Marking read success for %s %v\n", channel, itemUIDs)
}
