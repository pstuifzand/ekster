package main

import (
	"bufio"
	"bytes"
	"database/sql"
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

	"github.com/pkg/errors"
	"p83.nl/go/ekster/pkg/auth"
	"p83.nl/go/ekster/pkg/fetch"
	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/sse"
	"p83.nl/go/ekster/pkg/timeline"
	"p83.nl/go/ekster/pkg/util"

	"github.com/gomodule/redigo/redis"
	"willnorris.com/go/microformats"
)

// DefaultPrio is the priority value for new channels
const DefaultPrio = 9999999

type memoryBackend struct {
	hubIncomingBackend

	lock     sync.RWMutex
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	Settings map[string]channelSetting
	NextUID  int

	Me            string // FIXME: should be removed
	TokenEndpoint string // FIXME: should be removed
	AuthEnabled   bool

	ticker *time.Ticker
	quit   chan struct{}

	broker *sse.Broker

	pool *redis.Pool

	database *sql.DB
}

type channelSetting struct {
	ExcludeRegex string
	IncludeRegex string
	ExcludeType  []string
	ChannelType  string
}

type channelMessage struct {
	Version int              `json:"version"`
	Channel microsub.Channel `json:"channel"`
}

type channelDeletedMessage struct {
	Version int    `json:"version"`
	UID     string `json:"uid"`
}

type newItemMessage struct {
	Item    microsub.Item `json:"item"`
	Channel string        `json:"channel"`
}

// Debug interface for easy of use in other packages
type Debug interface {
	Debug()
}

type fetch2 struct{}

func (f *fetch2) Fetch(url string) (*http.Response, error) {
	return Fetch2(url)
}

func (b *memoryBackend) AuthTokenAccepted(header string, r *auth.TokenResponse) (bool, error) {
	conn := b.pool.Get()
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Printf("could not close redis connection: %v", err)
		}
	}()
	return cachedCheckAuthToken(conn, header, b.TokenEndpoint, r)
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
		return err
	}
	defer f.Close()
	jw := json.NewDecoder(f)
	return jw.Decode(b)
}

func (b *memoryBackend) refreshChannels() {
	conn := b.pool.Get()
	defer conn.Close()

	conn.Do("DEL", "channels")

	updateChannelInRedis(conn, "notifications", 1)

	b.lock.RLock()
	for uid, channel := range b.Channels {
		log.Printf("loading channel %s - %s\n", uid, channel.Name)
		updateChannelInRedis(conn, channel.UID, DefaultPrio)
	}

	b.lock.RUnlock()
}

func (b *memoryBackend) save() error {
	filename := "backend.json"
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	jw := json.NewEncoder(f)
	jw.SetIndent("", "    ")
	b.lock.RLock()
	defer b.lock.RUnlock()
	return jw.Encode(b)
}

func loadMemoryBackend(pool *redis.Pool, database *sql.DB) (*memoryBackend, error) {
	backend := &memoryBackend{pool: pool, database: database}
	err := backend.load()
	if err != nil {
		return nil, errors.Wrap(err, "while loading backend")
	}
	backend.refreshChannels()
	return backend, nil
}

func createMemoryBackend() error {
	backend := memoryBackend{}
	backend.lock.Lock()

	backend.Feeds = make(map[string][]microsub.Feed)
	channels := []microsub.Channel{
		{UID: "notifications", Name: "Notifications"},
		{UID: "home", Name: "Home"},
	}

	backend.Channels = make(map[string]microsub.Channel)
	for _, c := range channels {
		backend.Channels[c.UID] = c
	}

	backend.NextUID = 1000000
	// FIXME: can't be used in Backend
	backend.Me = "https://example.com/"

	backend.lock.Unlock()

	return backend.save()
}

// ChannelsGetList gets channels
func (b *memoryBackend) ChannelsGetList() ([]microsub.Channel, error) {
	conn := b.pool.Get()
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
	util.StablePartition(channels, 0, len(channels), func(i int) bool {
		return channels[i].Unread.HasUnread()
	})

	return channels, nil
}

// ChannelsCreate creates a channels
func (b *memoryBackend) ChannelsCreate(name string) (microsub.Channel, error) {
	// Try to fetch the channel, if it exists, we don't create it
	if channel, e := b.fetchChannel(name); e {
		return channel, nil
	}

	// Otherwise create the channel
	channel := b.createChannel(name)
	b.setChannel(channel)
	b.save()

	conn := b.pool.Get()
	defer conn.Close()

	updateChannelInRedis(conn, channel.UID, DefaultPrio)

	b.broker.Notifier <- sse.Message{Event: "new channel", Object: channelMessage{1, channel}}

	return channel, nil
}

// ChannelsUpdate updates a channels
func (b *memoryBackend) ChannelsUpdate(uid, name string) (microsub.Channel, error) {
	defer b.save()

	b.lock.RLock()
	c, e := b.Channels[uid]
	b.lock.RUnlock()

	if e {
		c.Name = name

		b.lock.Lock()
		b.Channels[uid] = c
		b.lock.Unlock()

		b.broker.Notifier <- sse.Message{Event: "update channel", Object: channelMessage{1, c}}

		return c, nil
	}

	return microsub.Channel{}, fmt.Errorf("channel %s does not exist", uid)
}

// ChannelsDelete deletes a channel
func (b *memoryBackend) ChannelsDelete(uid string) error {
	defer b.save()

	conn := b.pool.Get()
	defer conn.Close()

	removed := false

	b.lock.RLock()
	if _, e := b.Channels[uid]; e {
		removed = true
	}
	b.lock.RUnlock()

	removeChannelFromRedis(conn, uid)

	b.lock.Lock()
	delete(b.Channels, uid)
	delete(b.Feeds, uid)
	b.lock.Unlock()

	if removed {
		b.broker.Notifier <- sse.Message{Event: "delete channel", Object: channelDeletedMessage{1, uid}}
	}

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
				b.RefreshFeeds()

			case <-b.quit:
				b.ticker.Stop()
				return
			}
		}
	}()
}

func (b *memoryBackend) RefreshFeeds() {
	feeds := b.getFeeds()

	count := 0

	for uid := range feeds {
		for _, feedURL := range feeds[uid] {
			log.Println(feedURL)
			resp, err := b.Fetch3(uid, feedURL)
			if err != nil {
				_ = b.channelAddItem("notifications", microsub.Item{
					Type: "entry",
					Name: "Error while fetching feed",
					Content: &microsub.Content{
						Text: fmt.Sprintf("Error while updating feed %s: %v", feedURL, err),
					},
					UID: time.Now().String(),
				})
				count++
				log.Printf("Error while Fetch3 of %s: %v\n", feedURL, err)
				continue
			}
			_ = b.ProcessContent(uid, feedURL, resp.Header.Get("Content-Type"), resp.Body)
			_ = resp.Body.Close()
		}
	}

	if count > 0 {
		_ = b.updateChannelUnreadCount("notifications")
	}
}

func (b *memoryBackend) TimelineGet(before, after, channel string) (microsub.Timeline, error) {
	log.Printf("TimelineGet %s\n", channel)

	// Check if feed exists
	_, err := b.FollowGetList(channel)
	if err != nil {
		return microsub.Timeline{Items: []microsub.Item{}}, err
	}

	timelineBackend := b.getTimeline(channel)

	_ = b.updateChannelUnreadCount(channel)

	return timelineBackend.Items(before, after)
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
		_ = b.channelAddItem("notifications", microsub.Item{
			Type: "entry",
			Name: "Error while fetching feed",
			Content: &microsub.Content{
				Text: fmt.Sprintf("Error while Fetch3 of %s: %v", feed.URL, err),
			},
			UID: time.Now().String(),
		})
		_ = b.updateChannelUnreadCount("notifications")
		return feed, err
	}
	defer resp.Body.Close()

	b.lock.Lock()
	b.Feeds[uid] = append(b.Feeds[uid], feed)
	b.lock.Unlock()

	_ = b.ProcessContent(uid, feed.URL, resp.Header.Get("Content-Type"), resp.Body)

	_, _ = b.CreateFeed(url, uid)

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

func (b *memoryBackend) ItemSearch(channel, query string) ([]microsub.Item, error) {
	return querySearch(channel, query)
}

func (b *memoryBackend) Search(query string) ([]microsub.Feed, error) {
	urls := getPossibleURLs(query)

	// needs to be like this, because we get a null result otherwise in the json output
	feeds := []microsub.Feed{}

	cachingFetch := WithCaching(b.pool, Fetch2)

	for _, u := range urls {
		log.Println(u)
		resp, err := cachingFetch(u)
		if err != nil {
			log.Printf("Error while fetching %s: %v\n", u, err)
			continue
		}
		defer resp.Body.Close()

		fetchURL, err := url.Parse(u)
		md := microformats.Parse(resp.Body, fetchURL)
		if err != nil {
			log.Printf("Error while fetching %s: %v\n", u, err)
			continue
		}

		feedResp, err := cachingFetch(fetchURL.String())
		if err != nil {
			log.Printf("Error in fetch of %s - %v\n", fetchURL, err)
			continue
		}
		defer feedResp.Body.Close()

		// TODO: Combine FeedHeader and FeedItems so we can use it here
		parsedFeed, err := fetch.FeedHeader(cachingFetch, fetchURL.String(), feedResp.Header.Get("Content-Type"), feedResp.Body)
		if err != nil {
			log.Printf("Error in parse of %s - %v\n", fetchURL, err)
			continue
		}

		// TODO: Only include the feed if it contains some items
		feeds = append(feeds, parsedFeed)

		if alts, e := md.Rels["alternate"]; e {
			for _, alt := range alts {
				relURL := md.RelURLs[alt]
				log.Printf("alternate found with type %s %#v\n", relURL.Type, relURL)

				if strings.HasPrefix(relURL.Type, "text/html") || strings.HasPrefix(relURL.Type, "application/json") || strings.HasPrefix(relURL.Type, "application/xml") || strings.HasPrefix(relURL.Type, "text/xml") || strings.HasPrefix(relURL.Type, "application/rss+xml") || strings.HasPrefix(relURL.Type, "application/atom+xml") {
					feedResp, err := cachingFetch(alt)
					if err != nil {
						log.Printf("Error in fetch of %s - %v\n", alt, err)
						continue
					}
					// FIXME: don't defer in for loop (possible memory leak)
					defer feedResp.Body.Close()

					parsedFeed, err := fetch.FeedHeader(cachingFetch, alt, feedResp.Header.Get("Content-Type"), feedResp.Body)
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
	cachingFetch := WithCaching(b.pool, Fetch2)
	resp, err := cachingFetch(previewURL)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}
	defer resp.Body.Close()
	items, err := fetch.FeedItems(cachingFetch, previewURL, resp.Header.Get("content-type"), resp.Body)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}

	return microsub.Timeline{
		Items: items,
	}, nil
}

func (b *memoryBackend) MarkRead(channel string, uids []string) error {
	tl := b.getTimeline(channel)
	err := tl.MarkRead(uids)

	if err != nil {
		return err
	}

	return b.updateChannelUnreadCount(channel)
}

func (b *memoryBackend) Events() (chan sse.Message, error) {
	return sse.StartConnection(b.broker)
}

func (b *memoryBackend) ProcessContent(channel, fetchURL, contentType string, body io.Reader) error {
	cachingFetch := WithCaching(b.pool, Fetch2)

	items, err := fetch.FeedItems(cachingFetch, fetchURL, contentType, body)
	if err != nil {
		return err
	}

	for _, item := range items {
		item.Read = false
		err = b.channelAddItemWithMatcher(channel, item)
		if err != nil {
			log.Printf("ERROR: %s\n", err)
		}
	}

	return b.updateChannelUnreadCount(channel)
}

// Fetch3 fills stuff
func (b *memoryBackend) Fetch3(channel, fetchURL string) (*http.Response, error) {
	log.Printf("Fetching channel=%s fetchURL=%s\n", channel, fetchURL)
	return Fetch2(fetchURL)
}

func (b *memoryBackend) channelAddItemWithMatcher(channel string, item microsub.Item) error {
	// an item is posted
	// check for all channels as channel
	// if regex matches item
	//  - add item to channel

	err := addToSearch(item, channel)
	if err != nil {
		return fmt.Errorf("addToSearch in channelAddItemWithMatcher: %v", err)
	}

	var updatedChannels []string

	b.lock.RLock()
	settings := b.Settings
	b.lock.RUnlock()

	for channelKey, setting := range settings {
		if len(setting.ExcludeType) > 0 {
			for _, v := range setting.ExcludeType {
				switch v {
				case "repost":
					if len(item.RepostOf) > 0 {
						return nil
					}
					break
				case "like":
					if len(item.LikeOf) > 0 {
						return nil
					}
					break
				case "bookmark":
					if len(item.BookmarkOf) > 0 {
						return nil
					}
					break
				case "reply":
					if len(item.InReplyTo) > 0 {
						return nil
					}
					break
				case "checkin":
					if item.Checkin != nil {
						return nil
					}
					break
				}
			}
		}
		if setting.IncludeRegex != "" {
			re, err := regexp.Compile(setting.IncludeRegex)
			if err != nil {
				log.Printf("error in regexp: %q, %s\n", setting.IncludeRegex, err)
				return nil
			}

			if matchItem(item, re) {
				log.Printf("Included %#v\n", item)
				err := b.channelAddItem(channelKey, item)
				if err != nil {
					continue
				}
				updatedChannels = append(updatedChannels, channelKey)
			}
		}
	}

	// Update all channels that have added items, because of the include matching
	for _, value := range updatedChannels {
		err := b.updateChannelUnreadCount(value)
		if err != nil {
			log.Printf("error while updating unread count for %s: %s", value, err)
			continue
		}
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

	return b.channelAddItem(channel, item)
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

func (b *memoryBackend) channelAddItem(channel string, item microsub.Item) error {
	timelineBackend := b.getTimeline(channel)
	added, err := timelineBackend.AddItem(item)

	// Sent message to Server-Sent-Events
	if added {
		b.broker.Notifier <- sse.Message{Event: "new item", Object: newItemMessage{item, channel}}
	}

	return err
}

func (b *memoryBackend) updateChannelUnreadCount(channel string) error {
	b.lock.RLock()
	c, exists := b.Channels[channel]
	b.lock.RUnlock()

	if exists {
		tl := b.getTimeline(channel)
		unread, err := tl.Count()
		if err != nil {
			return err
		}
		defer b.save()

		currentCount := c.Unread.UnreadCount
		c.Unread = microsub.Unread{Type: microsub.UnreadCount, UnreadCount: unread}

		// Sent message to Server-Sent-Events
		if currentCount != unread {
			b.broker.Notifier <- sse.Message{Event: "new item in channel", Object: c}
		}

		b.lock.Lock()
		b.Channels[channel] = c
		b.lock.Unlock()
	}

	return nil
}

// WithCaching adds caching to a FetcherFunc
func WithCaching(pool *redis.Pool, ff fetch.FetcherFunc) fetch.FetcherFunc {
	conn := pool.Get()
	defer conn.Close()

	return func(fetchURL string) (*http.Response, error) {
		cacheKey := fmt.Sprintf("http_cache:%s", fetchURL)
		u, err := url.Parse(fetchURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
		}

		req, err := http.NewRequest("GET", u.String(), nil)

		data, err := redis.Bytes(conn.Do("GET", cacheKey))
		if err == nil {
			log.Printf("HIT %s\n", fetchURL)
			rd := bufio.NewReader(bytes.NewReader(data))
			return http.ReadResponse(rd, req)
		}

		log.Printf("MISS %s\n", fetchURL)

		resp, err := ff(fetchURL)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var b bytes.Buffer
		err = resp.Write(&b)
		if err != nil {
			return nil, err
		}

		cachedCopy := make([]byte, b.Len())
		cur := b.Bytes()
		copy(cachedCopy, cur)

		conn.Do("SET", cacheKey, cachedCopy, "EX", 60*60)

		cachedResp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(cachedCopy)), req)
		return cachedResp, err
	}
}

// Fetch2 fetches stuff
func Fetch2(fetchURL string) (*http.Response, error) {
	if !strings.HasPrefix(fetchURL, "http") {
		return nil, fmt.Errorf("error parsing %s as url, has no http(s) prefix", fetchURL)
	}

	u, err := url.Parse(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
	}

	req, err := http.NewRequest("GET", u.String(), nil)

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %s: %s", u, err)
	}

	return resp, err
}

func (b *memoryBackend) getTimeline(channel string) timeline.Backend {
	timelineType := "sorted-set"
	if channel == "notifications" {
		timelineType = "stream"
	} else {
		if setting, ok := b.Settings[channel]; ok {
			if setting.ChannelType != "" {
				timelineType = setting.ChannelType
			}
		}
	}

	tl := timeline.Create(channel, timelineType, b.pool, b.database)
	if tl == nil {
		log.Printf("no timeline found with name %q and type %q", channel, timelineType)
	}
	return tl
}

func (b *memoryBackend) createChannel(name string) microsub.Channel {
	uid := fmt.Sprintf("%012d", b.NextUID)
	channel := microsub.Channel{
		UID:    uid,
		Name:   name,
		Unread: microsub.Unread{Type: microsub.UnreadCount},
	}
	return channel
}

func (b *memoryBackend) fetchChannel(name string) (microsub.Channel, bool) {
	b.lock.RLock()
	defer b.lock.RUnlock()

	for _, c := range b.Channels {
		if c.Name == name {
			return c, true
		}
	}

	return microsub.Channel{}, false
}

func (b *memoryBackend) setChannel(channel microsub.Channel) {
	b.lock.Lock()
	defer b.lock.Unlock()

	b.Channels[channel.UID] = channel
	b.Feeds[channel.UID] = []microsub.Feed{}
	b.NextUID++
}

func updateChannelInRedis(conn redis.Conn, uid string, prio int) {
	conn.Do("SADD", "channels", uid)
	conn.Do("SETNX", "channel_sortorder_"+uid, prio)
}

func removeChannelFromRedis(conn redis.Conn, uid string) {
	conn.Do("SREM", "channels", uid)
	conn.Do("DEL", "channel_sortorder_"+uid)
}
