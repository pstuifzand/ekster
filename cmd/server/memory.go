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
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	NextUid  int

	ticker *time.Ticker
	quit   chan struct{}
}

type Debug interface {
	Debug()
}

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
}

func (b *memoryBackend) Debug() {
	fmt.Println(b.Channels)
}

func (b *memoryBackend) load() error {
	filename := "backend.json"
	f, err := os.Open(filename)
	if err != nil {
		panic("cant open backend.json")
	}
	defer f.Close()
	jw := json.NewDecoder(f)
	err = jw.Decode(b)
	if err != nil {
		return err
	}

	conn := pool.Get()
	defer conn.Close()

	conn.Do("SETNX", "channel_sortorder_notifications", 1)

	conn.Do("DEL", "channels")

	for uid, channel := range b.Channels {
		log.Printf("loading channel %s - %s\n", uid, channel.Name)
		for _, feed := range b.Feeds[uid] {
			log.Printf("- loading feed %s\n", feed.URL)
			resp, err := b.Fetch3(uid, feed.URL)
			if err != nil {
				log.Printf("Error while Fetch3 of %s: %v\n", feed.URL, err)
				continue
			}
			defer resp.Body.Close()
			b.ProcessContent(uid, feed.URL, resp.Header.Get("Content-Type"), resp.Body)
		}

		conn.Do("SADD", "channels", uid)
		conn.Do("SETNX", "channel_sortorder_"+uid, 99999)
	}
	return nil
}

func (b *memoryBackend) save() {
	filename := "backend.json"
	f, _ := os.Create(filename)
	defer f.Close()
	jw := json.NewEncoder(f)
	jw.Encode(b)
}

func loadMemoryBackend() microsub.Microsub {
	backend := &memoryBackend{}
	err := backend.load()
	if err != nil {
		log.Printf("Error while loadingbackend: %v\n", err)
		return nil
	}

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

// ChannelsGetList gets channels
func (b *memoryBackend) ChannelsGetList() []microsub.Channel {
	conn := pool.Get()
	defer conn.Close()

	channels := []microsub.Channel{}
	uids, err := redis.Strings(conn.Do("SORT", "channels", "BY", "channel_sortorder_*", "ASC"))
	if err != nil {
		log.Printf("Sorting channels failed: %v\n", err)
		for _, v := range b.Channels {
			channels = append(channels, v)
		}
	} else {
		for _, uid := range uids {
			channels = append(channels, b.Channels[uid])
		}
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

func mapToAuthor(result map[string]string) microsub.Card {
	item := microsub.Card{}
	item.Type = "card"
	if name, e := result["name"]; e {
		item.Name = name
	}
	if u, e := result["url"]; e {
		item.URL = u
	}
	if photo, e := result["photo"]; e {
		item.Photo = photo
	}
	if value, e := result["longitude"]; e {
		item.Longitude = value
	}
	if value, e := result["latitude"]; e {
		item.Latitude = value
	}
	if value, e := result["country-name"]; e {
		item.CountryName = value
	}
	if value, e := result["locality"]; e {
		item.Locality = value
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
		item.Author = mapToAuthor(author.(map[string]string))
	}

	if checkin, e := result["checkin"]; e {
		item.Checkin = mapToAuthor(checkin.(map[string]string))
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

	if value, e := result["like-of"]; e {
		for _, v := range value.([]interface{}) {
			if u, ok := v.(string); ok {
				item.LikeOf = append(item.LikeOf, u)
			}
		}
	}

	if value, e := result["repost-of"]; e {
		for _, v := range value.([]interface{}) {
			if u, ok := v.(string); ok {
				item.RepostOf = append(item.RepostOf, u)
			}
		}
	}

	if value, e := result["bookmark-of"]; e {
		for _, v := range value.([]interface{}) {
			if u, ok := v.(string); ok {
				item.BookmarkOf = append(item.BookmarkOf, u)
			}
		}
	}

	if value, e := result["in-reply-to"]; e {
		for _, v := range value.([]interface{}) {
			if replyTo, ok := v.(string); ok {
				item.InReplyTo = append(item.InReplyTo, replyTo)
			} else if cite, ok := v.(map[string]interface{}); ok {
				item.InReplyTo = append(item.InReplyTo, cite["url"].(string))
			}
		}
	}

	if value, e := result["photo"]; e {
		for _, v := range value.([]interface{}) {
			item.Photo = append(item.Photo, v.(string))
		}
	}

	if value, e := result["category"]; e {
		if cats, ok := value.([]string); ok {
			for _, v := range cats {
				item.Category = append(item.Category, v)
			}
		} else if cats, ok := value.([]interface{}); ok {
			for _, v := range cats {
				if cat, ok := v.(string); ok {
					item.Category = append(item.Category, cat)
				} else if cat, ok := v.(map[string]interface{}); ok {
					item.Category = append(item.Category, cat["value"].(string))
				}
			}
		} else if cat, ok := value.(string); ok {
			item.Category = append(item.Category, cat)
		}
	}

	if published, e := result["published"]; e {
		item.Published = published.(string)
	}

	if updated, e := result["updated"]; e {
		item.Updated = updated.(string)
	}

	if id, e := result["_id"]; e {
		item.ID = id.(string)
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
				for uid := range b.Channels {
					for _, feed := range b.Feeds[uid] {
						resp, err := b.Fetch3(uid, feed.URL)
						if err != nil {
							log.Printf("Error while Fetch3 of %s: %v\n", feed.URL, err)
							continue
						}
						defer resp.Body.Close()
						b.ProcessContent(uid, feed.URL, resp.Header.Get("Content-Type"), resp.Body)
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
	conn := pool.Get()
	defer conn.Close()

	log.Printf("TimelineGet %s\n", channel)
	feeds := b.FollowGetList(channel)
	log.Println(feeds)

	items := []microsub.Item{}

	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)
	//channelKey := fmt.Sprintf("channel:%s:posts", channel)

	//itemJsons, err := redis.ByteSlices(conn.Do("SORT", channelKey, "BY", "*->Published", "GET", "*->Data", "ASC", "ALPHA"))
	// if err != nil {
	// 	log.Println(err)
	// 	return microsub.Timeline{
	// 		Paging: microsub.Pagination{},
	// 		Items:  items,
	// 	}
	// }

	afterScore := "-inf"
	if len(after) != 0 {
		afterScore = "(" + after
	}
	beforeScore := "+inf"
	if len(before) != 0 {
		beforeScore = "(" + before
	}

	itemJSONs := [][]byte{}

	itemScores, err := redis.Strings(
		conn.Do(
			"ZRANGEBYSCORE",
			zchannelKey,
			afterScore,
			beforeScore,
			"LIMIT",
			0,
			20,
			"WITHSCORES",
		),
	)

	if err != nil {
		log.Println(err)
		return microsub.Timeline{
			Paging: microsub.Pagination{},
			Items:  items,
		}
	}

	if len(itemScores) >= 2 {
		before = itemScores[1]
		after = itemScores[len(itemScores)-1]
	}

	for i := 0; i < len(itemScores); i += 2 {
		itemID := itemScores[i]
		itemJSON, err := redis.Bytes(conn.Do("HGET", itemID, "Data"))
		if err != nil {
			log.Println(err)
			continue
		}
		itemJSONs = append(itemJSONs, itemJSON)
	}

	for _, obj := range itemJSONs {
		item := microsub.Item{}
		err := json.Unmarshal(obj, &item)
		if err != nil {
			log.Println(err)
			continue
		}
		item.Read = false
		items = append(items, item)
	}
	paging := microsub.Pagination{
		After:  after,
		Before: before,
	}

	return microsub.Timeline{
		Paging: paging,
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

// func (b *memoryBackend) checkRead(channel string, uid string) bool {
// 	conn := pool.Get()
// 	defer conn.Close()
// 	args := redis.Args{}.Add(fmt.Sprintf("timeline:%s:read", channel)).Add("item:" + uid)
// 	member, err := redis.Bool(conn.Do("SISMEMBER", args...))
// 	if err != nil {
// 		log.Printf("Checking read for channel %s item %s has failed\n", channel, uid)
// 	}
// 	return member
// }

// func (b *memoryBackend) wasRead(channel string, item map[string]interface{}) bool {
// 	if uid, e := item["uid"]; e {
// 		uid = hex.EncodeToString([]byte(uid.(string)))
// 		return b.checkRead(channel, uid.(string))
// 	}

// 	if uid, e := item["url"]; e {
// 		uid = hex.EncodeToString([]byte(uid.(string)))
// 		return b.checkRead(channel, uid.(string))
// 	}

// 	return false
// }

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
		log.Println(u)
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

		feedResp, err := Fetch2(fetchUrl.String())
		if err != nil {
			log.Printf("Error in fetch of %s - %v\n", fetchUrl, err)
			continue
		}
		defer feedResp.Body.Close()

		parsedFeed, err := b.feedHeader(fetchUrl.String(), feedResp.Header.Get("Content-Type"), feedResp.Body)
		if err != nil {
			log.Printf("Error in parse of %s - %v\n", fetchUrl, err)
			continue
		}

		feeds = append(feeds, parsedFeed)

		if alts, e := md.Rels["alternate"]; e {
			for _, alt := range alts {
				relURL := md.RelURLs[alt]
				log.Printf("alternate found with type %s %#v\n", relURL.Type, relURL)

				if strings.HasPrefix(relURL.Type, "text/html") || strings.HasPrefix(relURL.Type, "application/json") || strings.HasPrefix(relURL.Type, "application/xml") || strings.HasPrefix(relURL.Type, "text/xml") || strings.HasPrefix(relURL.Type, "application/rss+xml") || strings.HasPrefix(relURL.Type, "application/atom+xml") {
					feedResp, err := Fetch2(alt)
					if err != nil {
						log.Printf("Error in fetch of %s - %v\n", alt, err)
						continue
					}
					defer feedResp.Body.Close()

					parsedFeed, err := b.feedHeader(alt, feedResp.Header.Get("Content-Type"), feedResp.Body)
					if err != nil {
						log.Printf("Error in parse of %s - %v\n", alt, err)
						continue
					}

					feeds = append(feeds, parsedFeed)
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
	items, err := b.feedItems(previewURL, resp.Header.Get("content-type"), resp.Body)
	if err != nil {
		log.Printf("Error while fetching %s: %v\n", previewURL, err)
		return microsub.Timeline{}
	}

	return microsub.Timeline{
		Items: items,
	}
}

func (b *memoryBackend) MarkRead(channel string, uids []string) {
	conn := pool.Get()
	defer conn.Close()

	log.Printf("Marking read for %s %v\n", channel, uids)

	itemUIDs := []string{}
	for _, uid := range uids {
		itemUIDs = append(itemUIDs, "item:"+uid)
	}

	channelKey := fmt.Sprintf("zchannel:%s:posts", channel)
	args := redis.Args{}.Add(channelKey).AddFlat(itemUIDs)

	if _, err := conn.Do("ZREM", args...); err != nil {
		log.Printf("Marking read for channel %s has failed\n", channel)
	}

	log.Printf("Marking read success for %s %v\n", channel, itemUIDs)
}
