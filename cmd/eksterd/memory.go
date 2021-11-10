package main

import (
	"bufio"
	"bytes"
	"database/sql"
	"expvar"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"p83.nl/go/ekster/pkg/auth"
	"p83.nl/go/ekster/pkg/fetch"
	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/sse"
	"p83.nl/go/ekster/pkg/timeline"
	"p83.nl/go/ekster/pkg/util"

	"github.com/gomodule/redigo/redis"
	"willnorris.com/go/microformats"
)

var (
	varMicrosub *expvar.Map
)

func init() {
	varMicrosub = expvar.NewMap("microsub")
}

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

func loadMemoryBackend(pool *redis.Pool, database *sql.DB) (*memoryBackend, error) {
	backend := &memoryBackend{pool: pool, database: database}
	return backend, nil
}

// ChannelsGetList gets channels
func (b *memoryBackend) ChannelsGetList() ([]microsub.Channel, error) {
	conn := b.pool.Get()
	defer conn.Close()

	var channels []microsub.Channel
	rows, err := b.database.Query(`
SELECT c.uid, c.name, count(i.channel_id)
FROM "channels" "c" left join items i on c.id = i.channel_id and i.is_read = 0
GROUP BY c.id;
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid, name string
		var count int
		_ = rows.Scan(&uid, &name, &count)

		channels = append(channels, microsub.Channel{UID: uid, Name: name, Unread: microsub.Unread{
			Type:        microsub.UnreadCount,
			UnreadCount: count,
		}})
	}

	return channels, nil
}

func shouldRetryWithNewUID(err error, try int) bool {
	if err == nil {
		return false
	}

	if e, ok := err.(*pq.Error); ok {
		if e.Code == "23505" && e.Constraint == "channels_uid_key" {
			if try > 5 {
				return false
			}
			return true
		}
	}
	return false
}

// ChannelsCreate creates a channels
func (b *memoryBackend) ChannelsCreate(name string) (microsub.Channel, error) {
	varMicrosub.Add("ChannelsCreate", 1)
	/*
	 * try 5 times to generate a uid for a channel.
	 * If we get a database error we retry.
	 */
	try := 0
	channel := microsub.Channel{
		Name:   name,
		Unread: microsub.Unread{Type: microsub.UnreadCount},
	}
	for {
		varMicrosub.Add("ChannelsCreate.RandStringBytes", 1)
		channel.UID = util.RandStringBytes(24)
		result, err := b.database.Exec(`insert into "channels" ("uid", "name", "created_at") values($1, $2, DEFAULT)`, channel.UID, channel.Name)
		if err != nil {
			log.Println("channels insert", err)
			if !shouldRetryWithNewUID(err, try) {
				return channel, err
			}
			try++
			continue
		}
		if n, err := result.RowsAffected(); err != nil {
			if n > 0 {
				b.broker.Notifier <- sse.Message{Event: "new channel", Object: channelMessage{1, channel}}
			}
		}
		return channel, nil
	}
}

// ChannelsUpdate updates a channels
func (b *memoryBackend) ChannelsUpdate(uid, name string) (microsub.Channel, error) {
	_, err := b.database.Exec(`UPDATE "channels" SET "name" = $1 WHERE "uid" = $2`, name, uid)
	if err != nil {
		return microsub.Channel{}, err
	}
	c := microsub.Channel{
		UID:    uid,
		Name:   name,
		Unread: microsub.Unread{},
	}

	b.broker.Notifier <- sse.Message{Event: "update channel", Object: channelMessage{1, c}}

	return c, nil
}

// ChannelsDelete deletes a channel
func (b *memoryBackend) ChannelsDelete(uid string) error {
	_, err := b.database.Exec(`delete from "channels" where "uid" = $1`, uid)
	if err != nil {
		return err
	}
	b.broker.Notifier <- sse.Message{Event: "delete channel", Object: channelDeletedMessage{1, uid}}
	return nil
}

type feed struct {
	UID string // channel
	ID  int
	URL string
}

func (b *memoryBackend) getFeeds() ([]feed, error) {
	rows, err := b.database.Query(`SELECT "f"."id", "f"."url", "c"."uid" FROM "feeds" AS "f" INNER JOIN public.channels c on c.id = f.channel_id`)
	if err != nil {
		return nil, err
	}

	var feeds []feed
	for rows.Next() {
		var feedID int
		var feedURL, UID string

		err = rows.Scan(&feedID, &feedURL, &UID)
		if err != nil {
			log.Printf("while scanning feeds: %s", err)
			continue
		}

		feeds = append(feeds, feed{UID, feedID, feedURL})
	}

	return feeds, nil
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
	feeds, err := b.getFeeds()
	if err != nil {
		return
	}

	count := 0

	for _, feed := range feeds {
		feedURL := feed.URL
		feedID := feed.ID
		uid := feed.UID
		log.Println("Processing", feedURL)
		resp, err := b.Fetch3(uid, feedURL)
		if err != nil {
			log.Printf("Error while Fetch3 of %s: %v\n", feedURL, err)
			b.addNotification("Error while fetching feed", feedURL, err)
			count++
			continue
		}
		err = b.ProcessContent(uid, fmt.Sprintf("%d", feedID), feedURL, resp.Header.Get("Content-Type"), resp.Body)
		if err != nil {
			log.Printf("Error while processing content for %s: %v\n", feedURL, err)
			b.addNotification("Error while processing feed", feedURL, err)
			count++
			continue
		}
		_ = resp.Body.Close()
	}

	if count > 0 {
		_ = b.updateChannelUnreadCount("notifications")
	}
}

func (b *memoryBackend) addNotification(name string, feedURL string, err error) {
	err = b.channelAddItem("notifications", microsub.Item{
		Type: "entry",
		Name: name,
		Content: &microsub.Content{
			Text: fmt.Sprintf("ERROR: while updating feed: %s", err),
		},
		Published: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		log.Printf("ERROR: %s", err)
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
	rows, err := b.database.Query(`SELECT "f"."url" FROM "feeds" AS "f" INNER JOIN channels c on c.id = f.channel_id WHERE c.uid = $1`, uid)
	if err != nil {
		return nil, err
	}

	var feeds []microsub.Feed
	for rows.Next() {
		var feedURL string
		err = rows.Scan(&feedURL)
		if err != nil {
			continue
		}
		feeds = append(feeds, microsub.Feed{
			Type: "feed",
			URL:  feedURL,
		})
	}
	return feeds, nil
}

func (b *memoryBackend) FollowURL(uid string, url string) (microsub.Feed, error) {
	feed := microsub.Feed{Type: "feed", URL: url}

	var channelID int
	err := b.database.QueryRow(`SELECT "id" FROM "channels" WHERE "uid" = $1`, uid).Scan(&channelID)
	if err != nil {
		if err == sql.ErrNoRows {
			return microsub.Feed{}, fmt.Errorf("channel does not exist: %w", err)
		}
		return microsub.Feed{}, err
	}

	var feedID int
	err = b.database.QueryRow(
		`INSERT INTO "feeds" ("channel_id", "url") VALUES ($1, $2) RETURNING "id"`,
		channelID,
		feed.URL,
	).Scan(&feedID)
	if err != nil {
		return feed, err
	}

	resp, err := b.Fetch3(uid, feed.URL)
	if err != nil {
		log.Println(err)
		b.addNotification("Error while fetching feed", feed.URL, err)
		_ = b.updateChannelUnreadCount("notifications")
		return feed, err
	}
	defer resp.Body.Close()

	_ = b.ProcessContent(uid, fmt.Sprintf("%d", feedID), feed.URL, resp.Header.Get("Content-Type"), resp.Body)

	_, _ = b.CreateFeed(url)

	return feed, nil
}

func (b *memoryBackend) UnfollowURL(uid string, url string) error {
	_, err := b.database.Exec(`DELETE FROM "feeds" "f" USING "channels" "c" WHERE "c"."id" = "f"."channel_id" AND f.url = $1 AND c.uid = $2`, url, uid)
	return err
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

	cachingFetch := WithCaching(b.pool, fetch.FetcherFunc(Fetch2))

	for _, u := range urls {
		log.Println(u)
		resp, err := cachingFetch.Fetch(u)
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

		feedResp, err := cachingFetch.Fetch(fetchURL.String())
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
					feedResp, err := cachingFetch.Fetch(alt)
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
	cachingFetch := WithCaching(b.pool, fetch.FetcherFunc(Fetch2))
	resp, err := cachingFetch.Fetch(previewURL)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}
	defer resp.Body.Close()

	items, err := ProcessSourcedItems(cachingFetch, previewURL, resp.Header.Get("content-type"), resp.Body)
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

// ProcessSourcedItems processes items and adds the Source
func ProcessSourcedItems(fetcher fetch.Fetcher, fetchURL, contentType string, body io.Reader) ([]microsub.Item, error) {
	// When the source is available from the Header, we fill the Source of the item

	bodyBytes, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var source *microsub.Source
	if header, err := fetch.FeedHeader(fetcher, fetchURL, contentType, bytes.NewBuffer(bodyBytes)); err == nil {
		source = &microsub.Source{
			ID:    header.URL,
			URL:   header.URL,
			Name:  header.Name,
			Photo: header.Photo,
		}
	} else {
		source = &microsub.Source{
			ID:  fetchURL,
			URL: fetchURL,
		}
	}

	items, err := fetch.FeedItems(fetcher, fetchURL, contentType, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, err
	}

	for i, item := range items {
		item.Read = false
		item.Source = source
		items[i] = item
	}

	return items, nil
}

// ContentProcessor processes content for a channel and feed
type ContentProcessor interface {
	ProcessContent(channel, feedID, fetchURL, contentType string, body io.Reader) error
}

func (b *memoryBackend) ProcessContent(channel, feedID, fetchURL, contentType string, body io.Reader) error {
	cachingFetch := WithCaching(b.pool, fetch.FetcherFunc(Fetch2))

	items, err := ProcessSourcedItems(cachingFetch, fetchURL, contentType, body)
	if err != nil {
		return err
	}

	for _, item := range items {
		item.Source.ID = feedID
		err = b.channelAddItemWithMatcher(channel, item)
		if err != nil {
			log.Printf("ERROR: (feedID=%s) %s\n", feedID, err)
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
	if err != nil {
		return err
	}

	// Sent message to Server-Sent-Events
	if added {
		b.broker.Notifier <- sse.Message{Event: "new item", Object: newItemMessage{item, channel}}
	}

	return err
}

func (b *memoryBackend) updateChannelUnreadCount(channel string) error {
	// tl := b.getTimeline(channel)
	// unread, err := tl.Count()
	// if err != nil {
	// 	return err
	// }
	//
	// currentCount := c.Unread.UnreadCount
	// c.Unread = microsub.Unread{Type: microsub.UnreadCount, UnreadCount: unread}
	//
	// // Sent message to Server-Sent-Events
	// if currentCount != unread {
	// 	b.broker.Notifier <- sse.Message{Event: "new item in channel", Object: c}
	// }

	return nil
}

// WithCaching adds caching to a fetch.Fetcher
func WithCaching(pool *redis.Pool, ff fetch.Fetcher) fetch.Fetcher {
	ff2 := (func(fetchURL string) (*http.Response, error) {
		conn := pool.Get()
		defer conn.Close()

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

		resp, err := ff.Fetch(fetchURL)
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
	})
	return fetch.FetcherFunc(ff2)
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
		return nil, fmt.Errorf("fetch failed: %s", err)
	}

	return resp, err
}

func (b *memoryBackend) getTimeline(channel string) timeline.Backend {
	// Set a default timeline type if not set
	timelineType := "postgres-stream"
	// if setting, ok := b.Settings[channel]; ok && setting.ChannelType != "" {
	// 	timelineType = setting.ChannelType
	// }
	tl := timeline.Create(channel, timelineType, b.pool, b.database)
	if tl == nil {
		log.Printf("no timeline found with name %q and type %q", channel, timelineType)
	}
	return tl
}
