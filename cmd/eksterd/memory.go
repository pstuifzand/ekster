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
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"p83.nl/go/ekster/pkg/fetch"
	"p83.nl/go/ekster/pkg/microsub"

	"github.com/gomodule/redigo/redis"
	"willnorris.com/go/microformats"
)

type memoryBackend struct {
	lock     sync.RWMutex
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	Settings map[string]channelSetting
	NextUid  int

	Me            string
	TokenEndpoint string

	ticker *time.Ticker
	quit   chan struct{}
}

type channelSetting struct {
	ExcludeRegex string
	IncludeRegex string
}

type Debug interface {
	Debug()
}

type redisItem struct {
	ID        string
	Published string
	Read      bool
	Data      []byte
}

type fetch2 struct{}

func (f *fetch2) Fetch(url string) (*http.Response, error) {
	return Fetch2(url)
}

func (b *memoryBackend) Debug() {
	b.lock.RLock()
	defer b.lock.RUnlock()
	fmt.Println(b.Channels)
	fmt.Println(b.Feeds)
	fmt.Println(b.Settings)
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

	b.lock.RLock()
	defer b.lock.RUnlock()

	for uid, channel := range b.Channels {
		log.Printf("loading channel %s - %s\n", uid, channel.Name)
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
	jw.SetIndent("", "    ")
	b.lock.RLock()
	defer b.lock.RUnlock()
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
	backend.lock.Lock()

	backend.Channels = make(map[string]microsub.Channel)
	backend.Feeds = make(map[string][]microsub.Feed)
	channels := []microsub.Channel{
		{UID: "notifications", Name: "Notifications"},
		{UID: "home", Name: "Home"},
	}
	for _, c := range channels {
		backend.Channels[c.UID] = c
	}
	backend.NextUid = 1000000
	backend.Me = "https://example.com/"

	backend.lock.Unlock()

	backend.save()

	log.Println(`Config file "backend.json" is created in the current directory.`)
	log.Println(`Update "Me" variable to your website address "https://example.com/"`)
	log.Println(`Update "TokenEndpoint" variable to the address of your token endpoint "https://example.com/token"`)
	return &backend
}

// ChannelsGetList gets channels
func (b *memoryBackend) ChannelsGetList() ([]microsub.Channel, error) {
	conn := pool.Get()
	defer conn.Close()

	b.lock.RLock()
	defer b.lock.RUnlock()

	var channels []microsub.Channel
	uids, err := redis.Strings(conn.Do("SORT", "channels", "BY", "channel_sortorder_*", "ASC"))
	if err != nil {
		log.Printf("Sorting channels failed: %v\n", err)
		for _, v := range b.Channels {
			channels = append(channels, v)
		}
	} else {
		for _, uid := range uids {
			if c, e := b.Channels[uid]; e {
				channels = append(channels, c)
			}
		}
	}
	return channels, nil
}

// ChannelsCreate creates a channels
func (b *memoryBackend) ChannelsCreate(name string) (microsub.Channel, error) {
	defer b.save()

	conn := pool.Get()
	defer conn.Close()

	uid := fmt.Sprintf("%04d", b.NextUid)
	channel := microsub.Channel{
		UID:  uid,
		Name: name,
	}

	b.lock.Lock()
	b.Channels[channel.UID] = channel
	b.Feeds[channel.UID] = []microsub.Feed{}
	b.NextUid++
	b.lock.Unlock()

	conn.Do("SADD", "channels", uid)
	conn.Do("SETNX", "channel_sortorder_"+uid, 99999)

	return channel, nil
}

// ChannelsUpdate updates a channels
func (b *memoryBackend) ChannelsUpdate(uid, name string) (microsub.Channel, error) {
	defer b.save()

	b.lock.RLock()
	defer b.lock.RUnlock()

	b.lock.RLock()
	c, e := b.Channels[uid]
	b.lock.RUnlock()

	if e {
		c.Name = name

		b.lock.Lock()
		b.Channels[uid] = c
		b.lock.Unlock()

		return c, nil
	}

	return microsub.Channel{}, fmt.Errorf("Channel %s does not exist", uid)
}

// ChannelsDelete deletes a channel
func (b *memoryBackend) ChannelsDelete(uid string) error {
	defer b.save()

	conn := pool.Get()
	defer conn.Close()

	conn.Do("SREM", "channels", uid)
	conn.Do("DEL", "channel_sortorder_"+uid)

	b.lock.Lock()
	delete(b.Channels, uid)
	delete(b.Feeds, uid)
	b.lock.Unlock()

	return nil
}

func (b *memoryBackend) getFeeds() map[string][]string {
	feeds := make(map[string][]string)
	b.lock.RLock()
	for uid := range b.Channels {
		for _, feed := range b.Feeds[uid] {
			feeds[uid] = append(feeds[uid], feed.URL)
		}
	}
	b.lock.RUnlock()
	return feeds
}

func (b *memoryBackend) run() {
	b.ticker = time.NewTicker(10 * time.Minute)
	b.quit = make(chan struct{})

	go func() {
		for {
			select {
			case <-b.ticker.C:
				feeds := b.getFeeds()

				for uid := range feeds {
					for _, feedURL := range feeds[uid] {
						resp, err := b.Fetch3(uid, feedURL)
						if err != nil {
							log.Printf("Error while Fetch3 of %s: %v\n", feedURL, err)
							continue
						}
						defer resp.Body.Close()
						b.ProcessContent(uid, feedURL, resp.Header.Get("Content-Type"), resp.Body)
					}
				}

			case <-b.quit:
				b.ticker.Stop()
				return
			}
		}
	}()
}

func (b *memoryBackend) TimelineGet(before, after, channel string) (microsub.Timeline, error) {
	conn := pool.Get()
	defer conn.Close()

	log.Printf("TimelineGet %s\n", channel)
	feeds, err := b.FollowGetList(channel)
	if err != nil {
		return microsub.Timeline{}, err
	}
	log.Println(feeds)

	var items []microsub.Item

	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)

	afterScore := "-inf"
	if len(after) != 0 {
		afterScore = "(" + after
	}
	beforeScore := "+inf"
	if len(before) != 0 {
		beforeScore = "(" + before
	}

	var itemJSONs [][]byte

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
		return microsub.Timeline{
			Paging: microsub.Pagination{},
			Items:  items,
		}, err
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
			// FIXME: what should we do if one of the items doen't unmarshal?
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
	}, nil
}

func (b *memoryBackend) FollowGetList(uid string) ([]microsub.Feed, error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.Feeds[uid], nil
}

func (b *memoryBackend) FollowURL(uid string, url string) (microsub.Feed, error) {
	defer b.save()
	feed := microsub.Feed{Type: "feed", URL: url}

	resp, err := b.Fetch3(uid, feed.URL)
	if err != nil {
		return feed, err
	}
	defer resp.Body.Close()

	b.lock.Lock()
	b.Feeds[uid] = append(b.Feeds[uid], feed)
	b.lock.Unlock()

	b.ProcessContent(uid, feed.URL, resp.Header.Get("Content-Type"), resp.Body)

	return feed, nil
}

func (b *memoryBackend) UnfollowURL(uid string, url string) error {
	defer b.save()
	index := -1
	b.lock.Lock()
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
	b.lock.Unlock()

	return nil
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

func (b *memoryBackend) Search(query string) ([]microsub.Feed, error) {
	urls := getPossibleURLs(query)

	var feeds []microsub.Feed

	for _, u := range urls {
		log.Println(u)
		resp, err := Fetch2(u)
		if err != nil {
			log.Printf("Error while fetching %s: %v\n", u, err)
			continue
		}
		defer resp.Body.Close()

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

		parsedFeed, err := fetch.FeedHeader(&fetch2{}, fetchUrl.String(), feedResp.Header.Get("Content-Type"), feedResp.Body)
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
					// FIXME: don't defer in for loop (possible memory leak)
					defer feedResp.Body.Close()

					parsedFeed, err := fetch.FeedHeader(&fetch2{}, alt, feedResp.Header.Get("Content-Type"), feedResp.Body)
					if err != nil {
						log.Printf("Error in parse of %s - %v\n", alt, err)
						continue
					}

					feeds = append(feeds, parsedFeed)
				}
			}
		}
	}

	return feeds, nil
}

func (b *memoryBackend) PreviewURL(previewURL string) (microsub.Timeline, error) {
	resp, err := Fetch2(previewURL)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}
	defer resp.Body.Close()
	items, err := fetch.FeedItems(&fetch2{}, previewURL, resp.Header.Get("content-type"), resp.Body)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}

	return microsub.Timeline{
		Items: items,
	}, nil
}

func (b *memoryBackend) MarkRead(channel string, uids []string) error {
	conn := pool.Get()
	defer conn.Close()

	itemUIDs := []string{}
	for _, uid := range uids {
		itemUIDs = append(itemUIDs, "item:"+uid)
	}

	channelKey := fmt.Sprintf("channel:%s:read", channel)
	args := redis.Args{}.Add(channelKey).AddFlat(itemUIDs)

	if _, err := conn.Do("SADD", args...); err != nil {
		return fmt.Errorf("Marking read for channel %s has failed: %s", channel, err)
	}

	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)
	args = redis.Args{}.Add(zchannelKey).AddFlat(itemUIDs)

	if _, err := conn.Do("ZREM", args...); err != nil {
		return fmt.Errorf("Marking read for channel %s has failed: %s", channel, err)
	}

	err := b.updateChannelUnreadCount(conn, channel)
	if err != nil {
		return err
	}

	log.Printf("Marking read success for %s %v\n", channel, itemUIDs)

	return nil
}

func (b *memoryBackend) ProcessContent(channel, fetchURL, contentType string, body io.Reader) error {
	conn := pool.Get()
	defer conn.Close()

	items, err := fetch.FeedItems(&fetch2{}, fetchURL, contentType, body)
	if err != nil {
		return err
	}

	for _, item := range items {
		item.Read = false
		err = b.channelAddItemWithMatcher(conn, channel, item)
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

func (b *memoryBackend) channelAddItemWithMatcher(conn redis.Conn, channel string, item microsub.Item) error {
	// an item is posted
	// check for all channels as channel
	// if regex matches item
	//  - add item to channel

	var updatedChannels []string

	b.lock.RLock()
	settings := b.Settings
	b.lock.RUnlock()

	for channelKey, setting := range settings {
		if setting.IncludeRegex != "" {
			re, err := regexp.Compile(setting.IncludeRegex)
			if err != nil {
				log.Printf("error in regexp: %q, %s\n", setting.IncludeRegex, err)
				return nil
			}

			if matchItem(item, re) {
				log.Printf("Included %#v\n", item)
				b.channelAddItem(conn, channelKey, item)
				updatedChannels = append(updatedChannels, channelKey)
			}
		}
	}

	// Update all channels that have added items, because of the include matching
	for _, value := range updatedChannels {
		b.updateChannelUnreadCount(conn, value)
	}

	// Check for the exclude regex
	b.lock.RLock()
	setting, exists := b.Settings[channel]
	b.lock.RUnlock()

	if exists && setting.ExcludeRegex != "" {
		excludeRegex, err := regexp.Compile(setting.ExcludeRegex)
		if err != nil {
			log.Printf("error in regexp: %q\n", excludeRegex)
			return nil
		}
		if matchItem(item, excludeRegex) {
			log.Printf("Excluded %#v\n", item)
			return nil
		}
	}

	return b.channelAddItem(conn, channel, item)
}

func matchItem(item microsub.Item, re *regexp.Regexp) bool {
	if matchItemText(item, re) {
		return true
	}

	for _, v := range item.Refs {
		if matchItemText(v, re) {
			return true
		}
	}

	return false
}

func matchItemText(item microsub.Item, re *regexp.Regexp) bool {
	if item.Content != nil {
		if re.MatchString(item.Content.Text) {
			return true
		}
		if re.MatchString(item.Content.HTML) {
			return true
		}
	}
	return re.MatchString(item.Name)
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
	b.lock.RLock()
	c, exists := b.Channels[channel]
	b.lock.RUnlock()

	if exists {
		zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)
		unread, err := redis.Int(conn.Do("ZCARD", zchannelKey))
		if err != nil {
			return fmt.Errorf("error: while updating channel unread count for %s: %s", channel, err)
		}
		defer b.save()
		c.Unread = unread

		b.lock.Lock()
		b.Channels[channel] = c
		b.lock.Unlock()
	}

	return nil
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
	defer resp.Body.Close()

	var b bytes.Buffer
	resp.Write(&b)

	cachedCopy := make([]byte, b.Len())
	cur := b.Bytes()
	copy(cachedCopy, cur)

	conn.Do("SET", cacheKey, cachedCopy, "EX", 60*60)

	cachedResp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(cachedCopy)), req)
	return cachedResp, err
}
