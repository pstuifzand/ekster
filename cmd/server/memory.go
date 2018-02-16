package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/pstuifzand/microsub-server/microsub"
)

type memoryBackend struct {
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	//Items    map[string]map[string][]microsub.Item
	NextUid int
}

type Debug interface {
	Debug()
}

func (b *memoryBackend) Debug() {
	fmt.Println(b.Channels)
}

func (b *memoryBackend) load() {
	filename := "/tmp/backend.json"
	f, _ := os.Open(filename)
	defer f.Close()
	jw := json.NewDecoder(f)
	jw.Decode(b)
}

func (b *memoryBackend) save() {
	filename := "/tmp/backend.json"
	f, _ := os.Create(filename)
	defer f.Close()
	jw := json.NewEncoder(f)
	jw.Encode(b)
}

func loadMemoryBackend() microsub.Microsub {
	backend := &memoryBackend{}
	backend.load()

	return backend
}

func createMemoryBackend() microsub.Microsub {
	backend := memoryBackend{}
	defer backend.save()
	backend.Channels = make(map[string]microsub.Channel)
	backend.Feeds = make(map[string][]microsub.Feed)
	channels := []microsub.Channel{
		microsub.Channel{"0000", "default"},
		microsub.Channel{"0001", "notifications"},
		microsub.Channel{"1000", "Friends"},
		microsub.Channel{"1001", "Family"},
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

func (b *memoryBackend) TimelineGet(after, before, channel string) microsub.Timeline {
	feeds := b.FollowGetList(channel)

	items := []map[string]interface{}{}

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

			items = append(items, results...)
		}
	}

	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  items,
	}
}

func (b *memoryBackend) FollowGetList(uid string) []microsub.Feed {
	return b.Feeds[uid]
}

func (b *memoryBackend) FollowURL(uid string, url string) microsub.Feed {
	defer b.save()
	feed := microsub.Feed{"feed", url}
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

// TODO: improve search for feeds
func (b *memoryBackend) Search(query string) []microsub.Feed {
	return []microsub.Feed{
		microsub.Feed{"feed", query},
		//microsub.Feed{"feed", "https://peterstuifzand.nl/rss.xml"},
	}
}

func (b *memoryBackend) PreviewURL(previewUrl string) microsub.Timeline {
	md, err := Fetch2(previewUrl)
	if err != nil {
		return microsub.Timeline{}
	}
	results := simplifyMicroformatData(md)
	return microsub.Timeline{
		Items: results,
	}
}
