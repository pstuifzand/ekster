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
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/microsub-server/microsub"
)

type memoryBackend struct {
	Redis    redis.Conn
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	NextUid  int
}

type Debug interface {
	Debug()
}

func (b *memoryBackend) Debug() {
	fmt.Println(b.Channels)
}

func (b *memoryBackend) load() {
	filename := "backend.json"
	f, _ := os.Open(filename)
	defer f.Close()
	jw := json.NewDecoder(f)
	jw.Decode(b)
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
	if url, e := result["url"]; e {
		item.URL = url.(string)
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

	if published, e := result["published"]; e {
		item.Published = published.(string)
	}

	if updated, e := result["updated"]; e {
		item.Updated = updated.(string)
	}

	return item
}

func (b *memoryBackend) TimelineGet(after, before, channel string) microsub.Timeline {
	feeds := b.FollowGetList(channel)

	items := []microsub.Item{}

	for _, feed := range feeds {
		md, err := Fetch2(feed.URL)
		if err == nil {
			results := simplifyMicroformatData(md)

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
						md, _ := Fetch2(as)
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
				}
				if uid, e := r["url"]; e {
					r["_id"] = hex.EncodeToString([]byte(uid.(string)))
				}

				if _, e := r["published"]; e {
					item := mapToItem(r)
					items = append(items, item)
				}
			}
		}
	}

	sort.SliceStable(items, func(a, b int) bool {
		timeA := items[a].Published
		timeB := items[b].Published
		return strings.Compare(timeA, timeB) > 0
	})

	reverseSlice(items)

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
	args := redis.Args{}.Add(fmt.Sprintf("timeline:%s:read", channel)).Add(uid)
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

// TODO: improve search for feeds, perhaps even with mf2 parser
func (b *memoryBackend) Search(query string) []microsub.Feed {
	return []microsub.Feed{
		microsub.Feed{Type: "feed", URL: query},
	}
}

func (b *memoryBackend) PreviewURL(previewURL string) microsub.Timeline {
	md, err := Fetch2(previewURL)
	if err != nil {
		log.Println(err)
		return microsub.Timeline{}
	}
	results := simplifyMicroformatData(md)
	log.Println(results)
	items := []microsub.Item{}
	for _, r := range results {
		item := mapToItem(r)
		log.Println(item)
		items = append(items, item)
	}
	return microsub.Timeline{
		Items: items,
	}
}

func (b *memoryBackend) MarkRead(channel string, itemUids []string) {
	log.Printf("Marking read for %s %v\n", channel, itemUids)
	args := redis.Args{}.Add(fmt.Sprintf("timeline:%s:read", channel)).AddFlat(itemUids)
	if _, err := b.Redis.Do("SADD", args...); err != nil {
		log.Printf("Marking read for channel %s has failed\n", channel)
	}
	log.Printf("Marking read success for %s %v\n", channel, itemUids)
}
