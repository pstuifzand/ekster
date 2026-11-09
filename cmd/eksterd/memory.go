/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"expvar"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lib/pq"
	"github.com/pkg/errors"
	"github.com/pstuifzand/ekster/pkg/auth"
	"github.com/pstuifzand/ekster/pkg/fetch"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/observability"
	"github.com/pstuifzand/ekster/pkg/sse"
	"github.com/pstuifzand/ekster/pkg/timeline"
	"github.com/pstuifzand/ekster/pkg/userid"
	"github.com/pstuifzand/ekster/pkg/util"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

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
	lock     sync.RWMutex
	Channels map[string]microsub.Channel
	Feeds    map[string][]microsub.Feed
	Settings map[string]channelSetting
	NextUID  int

	AuthEnabled bool

	ticker *time.Ticker
	quit   chan struct{}

	broker *sse.Broker

	hubBackend HubBackend

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

type feed struct {
	UID         string // channel
	ID          int
	URL         string
	Tier        int
	Unmodified  int
	NextFetchAt time.Time
}

func (b *memoryBackend) AuthTokenAccepted(header string, r *auth.TokenResponse, endpoint string) (bool, error) {
	conn := b.pool.Get()
	defer func() {
		err := conn.Close()
		if err != nil {
			log.Printf("could not close redis connection: %v", err)
		}
	}()
	return cachedCheckAuthToken(conn, header, endpoint, r)
}

func loadMemoryBackend(pool *redis.Pool, database *sql.DB) (*memoryBackend, error) {
	backend := &memoryBackend{pool: pool, database: database}
	return backend, nil
}

// ChannelsGetList gets channels
func (b *memoryBackend) ChannelsGetList(ctx context.Context) ([]microsub.Channel, error) {
	userID, _ := userid.FromContext(ctx)

	var channels []microsub.Channel
	rows, err := b.database.Query(`
		SELECT c.uid, c.name, count(i.channel_id) as unread
		FROM "channels" "c" left join items i on c.id = i.channel_id and i.is_read = 0
		WHERE "c"."user_id" = $1
		GROUP BY c.id;
	`, userID)

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

	util.StablePartition(channels, 0, len(channels), func(i int) bool {
		return channels[i].Unread.HasUnread()
	})
	return channels, nil
}

func shouldRetryWithNewUID(err error, try int) bool {
	if err == nil {
		return false
	}

	if e, ok := err.(*pq.Error); ok {
		if e.Code == "23505" && e.Constraint == "channels_uid_key" {
			return try <= 5
		}
	}
	return false
}

// ChannelsCreate creates a channels
func (b *memoryBackend) ChannelsCreate(ctx context.Context, name string) (microsub.Channel, error) {
	varMicrosub.Add("ChannelsCreate", 1)

	userID, _ := userid.FromContext(ctx)

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
		result, err := b.database.Exec(
			`insert into "channels" ("uid", "name", "user_id", "created_at") values ($1, $2, $3, DEFAULT)`,
			channel.UID,
			channel.Name,
			userID,
		)
		if err != nil {
			log.Println("channels insert", err)
			if !shouldRetryWithNewUID(err, try) {
				return channel, err
			}
			try++
			continue
		}
		if n, err := result.RowsAffected(); err == nil {
			if n > 0 {
				b.broker.Notifier <- sse.Message{Event: "new channel", Object: channelMessage{1, channel}}
			}
		}
		return channel, nil
	}
}

// ChannelsUpdate updates a channels
func (b *memoryBackend) ChannelsUpdate(ctx context.Context, uid, name string) (microsub.Channel, error) {
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
func (b *memoryBackend) ChannelsDelete(ctx context.Context, uid string) error {
	_, err := b.database.Exec(`delete from "channels" where "uid" = $1`, uid)
	if err != nil {
		return err
	}
	b.broker.Notifier <- sse.Message{Event: "delete channel", Object: channelDeletedMessage{1, uid}}
	return nil
}
func (b *memoryBackend) updateFeed(feed feed) error {
	_, err := b.database.Exec(`
UPDATE "feeds"
SET "tier" = $2, "unmodified" = $3, "next_fetch_at" = $4
WHERE "id" = $1
`, feed.ID, feed.Tier, feed.Unmodified, feed.NextFetchAt)
	return err
}

func (b *memoryBackend) getFeeds() ([]feed, error) {
	rows, err := b.database.Query(`
SELECT "f"."id", "f"."url", "c"."uid", "f"."tier","f"."unmodified","f"."next_fetch_at"
FROM "feeds" AS "f"
INNER JOIN public.channels c ON c.id = f.channel_id
WHERE next_fetch_at IS NULL OR next_fetch_at < now()
`)
	if err != nil {
		return nil, err
	}

	var feeds []feed
	for rows.Next() {
		var feedID int
		var feedURL, UID string
		var tier, unmodified int
		var nextFetchAt sql.NullTime

		err = rows.Scan(&feedID, &feedURL, &UID, &tier, &unmodified, &nextFetchAt)
		if err != nil {
			log.Printf("while scanning feeds: %s", err)
			continue
		}

		var fetchTime time.Time
		if nextFetchAt.Valid {
			fetchTime = nextFetchAt.Time
		} else {
			fetchTime = time.Now()
		}

		feeds = append(
			feeds,
			feed{
				UID:         UID,
				ID:          feedID,
				URL:         feedURL,
				Tier:        tier,
				Unmodified:  unmodified,
				NextFetchAt: fetchTime,
			},
		)
	}

	return feeds, nil
}

func (b *memoryBackend) recordFeedOutcome(feedID, httpStatus int, fetchErr error, itemCount int, succeeded bool) {
	if succeeded {
		_, err := b.database.Exec(`
UPDATE "feeds"
SET "last_fetch_at" = now(), "last_http_status" = $2, "last_error" = NULL,
    "last_success_at" = now(), "last_item_count" = $3
WHERE "id" = $1
`, feedID, httpStatus, itemCount)
		if err != nil {
			log.Printf("could not record fetch outcome for feed %d: %s", feedID, err)
		}
		return
	}

	// A request that never produced a response has no status code; keep the
	// column NULL so the page can tell "the site is unreachable" from "the site
	// answered 404".
	var status sql.NullInt64
	if httpStatus != 0 {
		status = sql.NullInt64{Int64: int64(httpStatus), Valid: true}
	}

	message := ""
	if fetchErr != nil {
		message = fetchErr.Error()
	}

	_, err := b.database.Exec(`
UPDATE "feeds"
SET "last_fetch_at" = now(), "last_http_status" = $2, "last_error" = $3
WHERE "id" = $1
`, feedID, status, message)
	if err != nil {
		log.Printf("could not record fetch outcome for feed %d: %s", feedID, err)
	}
}

// feedStatus is one row of /status: the last fetch outcome for a feed, plus how
// much content the feed has ever produced, which is what separates a dormant
// blog from a feed that is being fetched and parsed into nothing.
type feedStatus struct {
	ID             int        `json:"id"`
	URL            string     `json:"url"`
	Channel        string     `json:"channel"`
	State          string     `json:"state"`
	LastFetchAt    *time.Time `json:"last_fetch_at,omitempty"`
	LastHTTPStatus *int       `json:"last_http_status,omitempty"`
	LastError      string     `json:"last_error,omitempty"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastItemCount  *int       `json:"last_item_count,omitempty"`
	NewestItemAt   *time.Time `json:"newest_item_at,omitempty"`
	TotalItems     int        `json:"total_items"`

	// Pre-formatted for the template; the JSON keeps the raw timestamps.
	LastFetch     string `json:"-"`
	LastSuccess   string `json:"-"`
	NewestItem    string `json:"-"`
	LastItemLabel string `json:"-"`
}

// FeedStatuses returns every feed with its last fetch outcome, newest first for
// the failing ones.
func (b *memoryBackend) FeedStatuses() ([]feedStatus, error) {
	rows, err := b.database.Query(`
SELECT "f"."id", "f"."url", "c"."name",
       "f"."last_fetch_at", "f"."last_http_status", "f"."last_error",
       "f"."last_success_at", "f"."last_item_count",
       (SELECT max("i"."published_at") FROM "items" AS "i"
          INNER JOIN "channels" AS "ic" ON "ic"."id" = "i"."channel_id"
         WHERE "i"."feed_id" = "f"."id" AND "ic"."uid" <> 'notifications'),
       (SELECT count(*) FROM "items" AS "i"
          INNER JOIN "channels" AS "ic" ON "ic"."id" = "i"."channel_id"
         WHERE "i"."feed_id" = "f"."id" AND "ic"."uid" <> 'notifications')
FROM "feeds" AS "f"
INNER JOIN "channels" AS "c" ON "c"."id" = "f"."channel_id"
ORDER BY "f"."url"
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var statuses []feedStatus
	for rows.Next() {
		var (
			s             feedStatus
			lastFetch     sql.NullTime
			lastStatus    sql.NullInt64
			lastError     sql.NullString
			lastSuccess   sql.NullTime
			lastItemCount sql.NullInt64
			newestItem    sql.NullTime
		)
		err = rows.Scan(&s.ID, &s.URL, &s.Channel,
			&lastFetch, &lastStatus, &lastError,
			&lastSuccess, &lastItemCount, &newestItem, &s.TotalItems)
		if err != nil {
			return nil, err
		}

		if lastFetch.Valid {
			t := lastFetch.Time
			s.LastFetchAt = &t
			s.LastFetch = age(t, now)
		}
		if lastStatus.Valid {
			code := int(lastStatus.Int64)
			s.LastHTTPStatus = &code
		}
		if lastError.Valid {
			s.LastError = lastError.String
		}
		if lastSuccess.Valid {
			t := lastSuccess.Time
			s.LastSuccessAt = &t
			s.LastSuccess = age(t, now)
		}
		if lastItemCount.Valid {
			n := int(lastItemCount.Int64)
			s.LastItemCount = &n
			s.LastItemLabel = fmt.Sprintf("%d", n)
		}
		if newestItem.Valid {
			t := newestItem.Time
			s.NewestItemAt = &t
			s.NewestItem = age(t, now)
		}

		s.State = classifyFeed(s.LastError != "", s.TotalItems, s.LastItemCount, s.NewestItemAt, now)

		statuses = append(statuses, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	severity := map[string]int{"failing": 0, "empty": 1, "stale": 2, "ok": 3}
	sort.SliceStable(statuses, func(i, j int) bool {
		if severity[statuses[i].State] != severity[statuses[j].State] {
			return severity[statuses[i].State] < severity[statuses[j].State]
		}
		return statuses[i].URL < statuses[j].URL
	})

	return statuses, nil
}

// staleAfter is how long a feed may go without a new item before /status calls
// it stale. It is a hint, not a verdict: plenty of blogs are simply quiet.
const staleAfter = 90 * 24 * time.Hour

// classifyFeed decides the label /status shows. A feed whose last fetch failed
// is "failing" whatever it stored before; one that is fetched but has produced
// no items is "empty", which is the shape of a parser that stopped matching the
// markup; a feed with old items and no error is merely "stale".
func classifyFeed(failed bool, totalItems int, lastItemCount *int, newestItemAt *time.Time, now time.Time) string {
	if failed {
		return "failing"
	}
	if totalItems == 0 {
		return "empty"
	}
	if lastItemCount != nil && *lastItemCount == 0 {
		return "empty"
	}
	if newestItemAt == nil || now.Sub(*newestItemAt) > staleAfter {
		return "stale"
	}
	return "ok"
}

func age(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < 0:
		return "in the future"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func (b *memoryBackend) run() {
	b.ticker = time.NewTicker(1 * time.Minute)
	b.quit = make(chan struct{})

	go func() {
		b.RefreshFeeds()

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
	log.Println("Feed update process started")
	defer log.Println("Feed update process completed")

	ctx, span := observability.Span(context.Background(), "feeds.refresh")
	defer span.End()

	feeds, err := b.getFeeds()
	if err != nil {
		span.RecordError(err)
		return
	}
	span.SetAttributes(attribute.Int("feeds.count", len(feeds)))

	log.Printf("Found %d feeds", len(feeds))

	count := 0
	for _, feed := range feeds {
		log.Println("Processing", feed.URL)
		terr := b.refreshFeedSafely(ctx, feed)
		if terr != nil {
			b.addNotification("Error while fetching feed", feed, terr)
			continue
		}

		count++
	}
	span.SetAttributes(attribute.Int("feeds.processed", count))

	if count > 0 {
		_ = b.updateChannelUnreadCount("notifications")
	}
	log.Printf("Processed %d feeds", count)
}

// refreshFeedSafely runs one feed's refresh and turns a panic into an error.
// A panic in a goroutine takes the whole daemon down, and the feed that
// triggers it here is fetched from the open web: its markup decides whether
// ekster keeps running. That is not a trade worth making, so the damage is
// limited to one feed and the next tick tries again.
func (b *memoryBackend) refreshFeedSafely(parent context.Context, f feed) (err error) {
	ctx, span := observability.Span(parent, "feed.refresh",
		attribute.String("feed.url", f.URL),
		attribute.String("feed.channel", f.UID),
		attribute.Int("feed.id", f.ID),
		attribute.Int("feed.tier", f.Tier),
	)
	defer func() {
		if r := recover(); r != nil {
			log.Printf("recovered while refreshing %s: %v\n%s", f.URL, r, debug.Stack())
			err = fmt.Errorf("panic while refreshing %s: %v", f.URL, r)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}()
	return b.refreshFeed(ctx, f)
}

func (b *memoryBackend) refreshFeed(ctx context.Context, feed feed) (err error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var (
		httpStatus int
		itemCount  int
		succeeded  bool
	)
	// Whatever happens below, the outcome is written to the feed row. A feed
	// that dies has to be a row someone can look at, not a log line nobody
	// reads; /status is built from these columns.
	defer func() {
		b.recordFeedOutcome(feed.ID, httpStatus, err, itemCount, succeeded)
	}()

	resp, err := b.Fetch3(ctx, feed.UID, feed.URL)
	if err != nil {
		return fmt.Errorf("while Fetch3 of %s: %w", feed.URL, err)
	}
	defer resp.Body.Close()
	httpStatus = resp.StatusCode

	// An error page is not a feed. Parsing a 404 body yielded zero items and
	// looked like a quiet blog, which is exactly the case this page exists for.
	if httpStatus < 200 || httpStatus > 299 {
		return fmt.Errorf("fetch of %s returned %s", feed.URL, resp.Status)
	}

	var changed bool
	changed, itemCount, err = b.ProcessContent(ctx, feed.UID, fmt.Sprintf("%d", feed.ID), feed.URL, resp.Header.Get("Content-Type"), resp.Body)
	if err != nil {
		return fmt.Errorf("in ProcessContent of %s: %w", feed.URL, err)
	}
	succeeded = true

	trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("feed.changed", changed))

	if changed {
		feed.Tier--
	} else {
		feed.Unmodified++
	}

	if feed.Unmodified >= 2 {
		feed.Tier++
		feed.Unmodified = 0
	}

	if feed.Tier > 10 {
		feed.Tier = 10
	}

	if feed.Tier < 0 {
		feed.Tier = 0
	}

	minutes := time.Duration(math.Ceil(math.Exp2(float64(feed.Tier))))

	feed.NextFetchAt = time.Now().Add(minutes * time.Minute)

	log.Printf("Next Fetch in %d minutes at %v", minutes, feed.NextFetchAt.Format(time.RFC3339))

	err = b.updateFeed(feed)
	if err != nil {
		log.Printf("Error: while updating feed %v: %v", feed, err)
		// don't return error, because it becomes a notification
		return nil
	}

	return nil
}

func (b *memoryBackend) addNotification(name string, feed feed, err error) {
	_, err = b.channelAddItem("notifications", microsub.Item{
		Type: "entry",
		Source: &microsub.Source{
			ID:   strconv.Itoa(feed.ID),
			URL:  feed.URL,
			Name: feed.URL,
		},
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

func (b *memoryBackend) TimelineGet(ctx context.Context, before, after, channel string) (microsub.Timeline, error) {
	log.Printf("TimelineGet %s\n", channel)

	// Check if feed exists
	_, err := b.FollowGetList(ctx, channel)
	if err != nil {
		return microsub.Timeline{Items: []microsub.Item{}}, err
	}

	timelineBackend, err := b.getTimeline(channel)
	if err != nil {
		return microsub.Timeline{}, err
	}

	// _ = b.updateChannelUnreadCount(channel)

	return timelineBackend.Items(before, after)
}

func (b *memoryBackend) FollowGetList(ctx context.Context, uid string) ([]microsub.Feed, error) {
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

func (b *memoryBackend) FollowURL(ctx context.Context, uid string, url string) (microsub.Feed, error) {
	subFeed := microsub.Feed{Type: "feed", URL: url}

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
		`INSERT INTO "feeds" ("channel_id", "url", "tier", "unmodified", "next_fetch_at") VALUES ($1, $2, 1, 0, now()) RETURNING "id"`,
		channelID,
		subFeed.URL,
	).Scan(&feedID)
	if err != nil {
		return subFeed, err
	}

	var newFeed = feed{
		ID:          feedID,
		UID:         uid,
		URL:         url,
		Tier:        1,
		Unmodified:  0,
		NextFetchAt: time.Now(),
	}
	resp, err := b.Fetch3(ctx, uid, subFeed.URL)
	if err != nil {
		log.Println(err)
		b.addNotification("Error while fetching feed", newFeed, err)
		_ = b.updateChannelUnreadCount("notifications")
		return subFeed, err
	}
	defer resp.Body.Close()

	_, _, _ = b.ProcessContent(ctx, uid, fmt.Sprintf("%d", feedID), subFeed.URL, resp.Header.Get("Content-Type"), resp.Body)

	_, _ = b.hubBackend.CreateFeed(url)

	return subFeed, nil
}

func (b *memoryBackend) UnfollowURL(ctx context.Context, uid string, url string) error {
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

func (b *memoryBackend) ItemSearch(ctx context.Context, channel, query string) ([]microsub.Item, error) {
	ids, err := querySearch(channel, query)
	if err != nil {
		return nil, fmt.Errorf("querySearch failed: %w", err)
	}

	tl, err := b.getTimeline(channel)
	if err != nil {
		return nil, fmt.Errorf("querySearch failed: %w", err)
	}
	return tl.ItemsByUID(ids)
}

func (b *memoryBackend) Search(ctx context.Context, query string) ([]microsub.Feed, error) {
	ctx, span := observability.Span(ctx, "feeds.search", attribute.String("search.query", query))
	defer span.End()

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
		parsedFeed, err := fetch.FeedHeader(ctx, cachingFetch, fetchURL.String(), feedResp.Header.Get("Content-Type"), feedResp.Body)
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

					parsedFeed, err := fetch.FeedHeader(ctx, cachingFetch, alt, feedResp.Header.Get("Content-Type"), feedResp.Body)
					if err != nil {
						log.Printf("Error in parse of %s - %v\n", alt, err)
						continue
					}

					feeds = append(feeds, parsedFeed)
				}
			}
		}
	}

	span.SetAttributes(attribute.Int("feeds.found", len(feeds)))
	return feeds, nil
}

func (b *memoryBackend) PreviewURL(ctx context.Context, previewURL string) (microsub.Timeline, error) {
	ctx, span := observability.Span(ctx, "feed.preview", attribute.String("feed.url", previewURL))
	defer span.End()

	cachingFetch := WithCaching(b.pool, fetch.FetcherFunc(Fetch2))
	resp, err := cachingFetch.Fetch(previewURL)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}
	defer resp.Body.Close()

	items, err := ProcessSourcedItems(ctx, cachingFetch, previewURL, resp.Header.Get("content-type"), resp.Body)
	if err != nil {
		return microsub.Timeline{}, fmt.Errorf("error while fetching %s: %v", previewURL, err)
	}

	return microsub.Timeline{
		Items: items,
	}, nil
}

func (b *memoryBackend) MarkRead(ctx context.Context, channel string, uids []string) error {
	tl, err := b.getTimeline(channel)
	if err != nil {
		return err
	}

	if err = tl.MarkRead(uids); err != nil {
		return err
	}

	if err = b.updateChannelUnreadCount(channel); err != nil {
		return err
	}

	return nil
}

func (b *memoryBackend) Events(ctx context.Context) (chan sse.Message, error) {
	return sse.StartConnection(b.broker)
}

// ProcessSourcedItems processes items and adds the Source
func ProcessSourcedItems(ctx context.Context, fetcher fetch.Fetcher, fetchURL, contentType string, body io.Reader) ([]microsub.Item, error) {
	// When the source is available from the Header, we fill the Source of the item

	ctx, span := observability.Span(ctx, "feed.parse",
		attribute.String("feed.url", fetchURL),
		attribute.String("feed.content_type", contentType),
		attribute.String("feed.format", fetch.Format(contentType)),
	)
	defer span.End()

	bodyBytes, err := ioutil.ReadAll(body)
	if err != nil {
		return nil, err
	}

	var source *microsub.Source
	if header, err := fetch.FeedHeader(ctx, fetcher, fetchURL, contentType, bytes.NewBuffer(bodyBytes)); err == nil {
		source = &microsub.Source{
			ID:    header.URL,
			URL:   header.URL,
			Name:  header.Name,
			Photo: header.Photo,
		}
		span.SetAttributes(attribute.String("feed.name", header.Name))
	} else {
		source = &microsub.Source{
			ID:  fetchURL,
			URL: fetchURL,
		}
	}

	items, err := fetch.FeedItems(ctx, fetcher, fetchURL, contentType, bytes.NewBuffer(bodyBytes))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}
	span.SetAttributes(attribute.Int("feed.items", len(items)))

	// A feed is allowed to list only titles, and an author is allowed to be a
	// reference to an h-card rather than the card itself. Both are the entry's
	// own markup to supply, so go and read it before the item is stored.
	resolver := newCardResolver(fetcher)
	for i, item := range items {
		item = enrichItem(ctx, fetcher, resolver, item)
		item.Read = false
		item.Source = source
		items[i] = item
	}

	return items, nil
}

// ContentProcessor processes content for a channel and feed
type ContentProcessor interface {
	ProcessContent(ctx context.Context, channel, feedID, fetchURL, contentType string, body io.Reader) (changed bool, items int, err error)
}

// ProcessContent processes content of a feed, returns if the feed has changed or not
func (b *memoryBackend) ProcessContent(ctx context.Context, channel, feedID, fetchURL, contentType string, body io.Reader) (changed bool, itemCount int, err error) {
	ctx, span := observability.Span(ctx, "feed.process",
		attribute.String("feed.channel", channel),
		attribute.String("feed.id", feedID),
		attribute.String("feed.url", fetchURL),
		attribute.String("feed.content_type", contentType),
	)
	defer func() {
		span.SetAttributes(
			attribute.Bool("feed.changed", changed),
			attribute.Int("feed.items", itemCount),
		)
		if err != nil {
			span.RecordError(err)
		}
		span.End()
	}()

	cachingFetch := WithCaching(b.pool, fetch.FetcherFunc(Fetch2))

	items, err := ProcessSourcedItems(ctx, cachingFetch, fetchURL, contentType, body)
	if err != nil {
		return false, 0, err
	}

	// The count is returned even when nothing changed: a document that parses
	// into zero items is the signature of a broken feed, and the caller records
	// it so /status can show it instead of the feed looking quiet.
	itemCount = len(items)

	for _, item := range items {
		item.Source.ID = feedID
		added, err := b.channelAddItemWithMatcher(channel, item)
		if err != nil {
			log.Printf("ERROR: (feedID=%s) %s\n", feedID, err)
		}
		changed = changed || added
	}

	err = b.updateChannelUnreadCount(channel)
	if err != nil {
		return changed, itemCount, err
	}

	return changed, itemCount, nil
}

// Fetch3 fills stuff
func (b *memoryBackend) Fetch3(ctx context.Context, channel, fetchURL string) (*http.Response, error) {
	log.Printf("Fetching channel=%s fetchURL=%s\n", channel, fetchURL)
	return Fetch2(ctx, fetchURL)
}

func (b *memoryBackend) channelAddItemWithMatcher(channel string, item microsub.Item) (bool, error) {
	// an item is posted
	// check for all channels as channel
	// if regex matches item
	//  - add item to channel

	var updatedChannels []string

	// FIXME: implement include regex
	// for channelKey, setting := range settings {
	// 	if setting.IncludeRegex != "" {
	// 		re, err := regexp.Compile(setting.IncludeRegex)
	// 		if err != nil {
	// 			log.Printf("error in regexp: %q, %s\n", setting.IncludeRegex, err)
	// 			return false, nil
	// 		}
	//
	// 		if matchItem(item, re) {
	// 			log.Printf("Included %#v\n", item)
	// 			added, err := b.channelAddItem(channelKey, item)
	// 			if err != nil {
	// 				continue
	// 			}
	//
	// 			err = addToSearch(item, channel)
	// 			if err != nil {
	// 				return added, fmt.Errorf("addToSearch in channelAddItemWithMatcher: %v", err)
	// 			}
	//
	// 			if added {
	// 				updatedChannels = append(updatedChannels, channelKey)
	// 			}
	// 		}
	// 	}
	// }

	// Update all channels that have added items, because the include_regex matches
	for _, value := range updatedChannels {
		err := b.updateChannelUnreadCount(value)
		if err != nil {
			log.Printf("error while updating unread count for %s: %s", value, err)
			continue
		}
	}

	// Check for the exclude regex
	setting, _ := b.loadSetting(channel)

	if len(setting.ExcludeType) > 0 {
		for _, v := range setting.ExcludeType {
			switch v {
			case "repost":
				if len(item.RepostOf) > 0 {
					return false, nil
				}
			case "like":
				if len(item.LikeOf) > 0 {
					return false, nil
				}
			case "bookmark":
				if len(item.BookmarkOf) > 0 {
					return false, nil
				}
			case "reply":
				if len(item.InReplyTo) > 0 {
					return false, nil
				}
			case "checkin":
				if item.Checkin != nil {
					return false, nil
				}
			}
		}
	}

	if setting.ExcludeRegex != "" {
		excludeRegex, err := regexp.Compile(setting.ExcludeRegex)
		if err != nil {
			log.Printf("error in regexp: %q\n", excludeRegex)
			return false, nil
		}
		if matchItem(item, excludeRegex) {
			log.Printf("Excluded %#v\n", item)
			return false, nil
		}
	}

	added, err := b.channelAddItem(channel, item)

	if err != nil {
		return added, err
	}

	err = addToSearch(item, channel)
	if err != nil {
		return added, fmt.Errorf("addToSearch in channelAddItemWithMatcher: %v", err)
	}

	return added, nil
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

func (b *memoryBackend) channelAddItem(channel string, item microsub.Item) (bool, error) {
	timelineBackend, err := b.getTimeline(channel)
	if err != nil {
		return false, err
	}

	added, err := timelineBackend.AddItem(item)
	if err != nil {
		return added, err
	}

	// Sent message to Server-Sent-Events
	if added {
		b.broker.Notifier <- sse.Message{Event: "new item", Object: newItemMessage{item, channel}}
	}

	return added, err
}

// ErrNotUpdated is used when the unread count is not updated
var ErrNotUpdated = errors.New("timeline unread count not updated")

// ErrNotFound is used when the timeline is not found
var ErrNotFound = errors.New("timeline not found")

func (b *memoryBackend) updateChannelUnreadCount(channel string) error {
	tl, err := b.getTimeline(channel)
	if err != nil {
		return err
	}

	unread, err := tl.Count()
	if err != nil {
		return ErrNotUpdated
	}

	var c = microsub.Channel{
		UID:    channel,
		Unread: microsub.Unread{Type: microsub.UnreadCount, UnreadCount: unread},
	}

	// Sent message to Server-Sent-Events
	b.broker.Notifier <- sse.Message{Event: "new item in channel", Object: c}

	return nil
}

// WithCaching adds caching to a fetch.Fetcher
func WithCaching(pool *redis.Pool, ff fetch.Fetcher) fetch.Fetcher {
	ff2 := (func(ctx context.Context, fetchURL string) (*http.Response, error) {
		conn := pool.Get()
		defer conn.Close()

		cacheKey := fmt.Sprintf("http_cache:%s", fetchURL)
		u, err := url.Parse(fetchURL)
		if err != nil {
			return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}

		data, err := redis.Bytes(conn.Do("GET", cacheKey))
		if err == nil {
			log.Printf("HIT %s\n", fetchURL)
			trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("http.cache.hit", true))
			rd := bufio.NewReader(bytes.NewReader(data))
			return http.ReadResponse(rd, req)
		}

		log.Printf("MISS %s\n", fetchURL)
		trace.SpanFromContext(ctx).SetAttributes(attribute.Bool("http.cache.hit", false))

		resp, err := ff.FetchWithContext(ctx, fetchURL)
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

// ErrBlackList returned when Url is not something we should fetch
var ErrBlackList = errors.New("Blacklisted URL")

// Fetch2 fetches stuff
func Fetch2(ctx context.Context, fetchURL string) (*http.Response, error) {
	if !strings.HasPrefix(fetchURL, "http") {
		return nil, fmt.Errorf("error parsing %s as url, has no http(s) prefix", fetchURL)
	}

	u, err := url.Parse(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("error parsing %s as url: %s", fetchURL, err)
	}

	if strings.Contains(u.Host, "twitter.com") {
		if !strings.Contains(u.Path, "/status/") {
			return nil, ErrBlackList
		}
	}
	if strings.Contains(u.Host, "reddit.com") {
		return nil, ErrBlackList
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %s", err)
	}

	return resp, err
}

func (b *memoryBackend) loadSetting(uid string) (channelSetting, error) {
	row := b.database.QueryRow(`SELECT "id" FROM "channels" WHERE "uid" = $1`, uid)
	var channelID int
	err := row.Scan(&channelID)
	if err != nil {
		return channelSetting{}, err
	}

	var settingsID int
	var setting channelSetting

	row = b.database.QueryRow(`SELECT "id", "settings" FROM "channel_settings" WHERE "channel_id" = $1`, channelID)

	err = row.Scan(&settingsID, &setting)
	if err == sql.ErrNoRows {
		return channelSetting{}, nil
	}
	if err != nil {
		return channelSetting{}, err
	}

	return setting, nil
}

func (b *memoryBackend) saveSetting(uid string, setting channelSetting) error {
	row := b.database.QueryRow(`SELECT "id" FROM "channels" WHERE "uid" = $1`, uid)
	var channelID int
	err := row.Scan(&channelID)
	if err != nil {
		return err
	}

	_, err = b.database.Exec(`
INSERT INTO "channel_settings" (channel_id, settings, created_at)
VALUES ($1, $2, now())
ON CONFLICT (channel_id) DO UPDATE
  SET settings = excluded.settings,
      updated_at = now()
`, channelID, &setting)

	return err
}

func (b *memoryBackend) getTimeline(channel string) (timeline.Backend, error) {
	// Set a default timeline type if not set
	timelineType := "postgres-stream"
	tl := timeline.Create(channel, timelineType, b.pool, b.database)
	if tl == nil {
		return tl, fmt.Errorf("timeline id %q: %w", channel, ErrNotFound)
	}
	return tl, nil
}

// Scan helps to scan json data from database
func (item *channelSetting) Scan(value interface{}) error {
	b, ok := value.([]byte)
	if !ok {
		return errors.New("type assertion to []byte failed")
	}

	return json.Unmarshal(b, &item)
}

// Value helps to add json data to database
func (item *channelSetting) Value() (driver.Value, error) {
	return json.Marshal(item)
}
