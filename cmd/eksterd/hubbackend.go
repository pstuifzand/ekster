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
	"bytes"
	"context"
	"database/sql"
	"expvar"
	"fmt"
	"io"
	"io/ioutil"
	"log/slog"
	"strings"
	"time"

	"github.com/pstuifzand/ekster/pkg/util"
	"github.com/pstuifzand/ekster/pkg/websub"

	"github.com/gomodule/redigo/redis"
)

// LeaseSeconds is the default number of seconds we want the subscription to last
const LeaseSeconds = 24 * 60 * 60

// HubBackend handles information for the incoming handler
type HubBackend interface {
	Feeds() ([]Feed, error)
	CreateFeed(url, token string) (int64, error)
	GetSecret(feedID int64) string
	UpdateFeed(ctx context.Context, processor ContentProcessor, feedID int64, contentType string, body io.Reader) error
	FeedSetLeaseSeconds(feedID int64, leaseSeconds int64) error
	Subscribe(feed *Feed) error
}

type hubIncomingBackend struct {
	baseURL  string
	pool     *redis.Pool
	database *sql.DB
}

// Feed contains information about the feed subscriptions
type Feed struct {
	ID            int64
	URL           string
	Callback      string
	Hub           string
	Secret        string
	LeaseSeconds  int64
	ResubscribeAt *time.Time
}

var (
	varWebsub *expvar.Map
)

func init() {
	varWebsub = expvar.NewMap("websub")
}

func (h *hubIncomingBackend) GetSecret(id int64) string {
	db := h.database
	var secret string
	err := db.QueryRow(
		`select "subscription_secret" from "subscriptions" where "id" = $1`,
		id,
	).Scan(&secret)
	if err != nil {
		return ""
	}
	return secret
}

func (h *hubIncomingBackend) CreateFeed(topic, token string) (int64, error) {
	slog.Debug("websub: creating feed", "topic", topic)
	db := h.database

	secret := util.RandStringBytes(32)
	urlSecret := util.RandStringBytes(32)

	var subscriptionID int
	err := db.QueryRow(`
INSERT INTO "subscriptions" ("topic","subscription_secret", "url_secret", "lease_seconds", "created_at")
VALUES ($1, $2, $3, $4, DEFAULT) RETURNING "id"`, topic, secret, urlSecret, 60*60*24*7).Scan(&subscriptionID)
	if err != nil {
		return 0, err
	}
	if err != nil {
		return 0, fmt.Errorf("insert into subscriptions: %w", err)
	}

	// Discovery reads the topic to find its hub, so a private topic answers 401
	// to an anonymous HEAD too. The channel's token is what the link lives
	// behind; without it the subscription is skipped and the feed only ever gets
	// polled.
	client := identifyingClient(0)
	if token != "" {
		client = tokenClient(token)
	}

	hubURL, err := websub.GetHubURL(client, topic)
	if err != nil {
		slog.Warn("websub: no hub url for topic", "topic", topic)
		return 0, err
	}

	callbackURL := fmt.Sprintf("%s/incoming/%d", h.baseURL, subscriptionID)

	slog.Info("websub: hub found", "topic", topic, "hub", hubURL, "callback", callbackURL)

	if err == nil && hubURL != "" {
		_, err := db.Exec(`UPDATE subscriptions SET hub = $1, callback = $2 WHERE id = $3`, hubURL, callbackURL, subscriptionID)
		if err != nil {
			return 0, fmt.Errorf("save hub and callback: %w", err)
		}
	} else {
		return int64(subscriptionID), nil
	}

	err = websub.Subscribe(client, hubURL, topic, callbackURL, secret, 24*3600)
	if err != nil {
		return 0, fmt.Errorf("subscribe: %w", err)
	}

	return int64(subscriptionID), nil
}

func (h *hubIncomingBackend) UpdateFeed(ctx context.Context, processor ContentProcessor, subscriptionID int64, contentType string, body io.Reader) error {
	slog.Debug("websub: updating feed", "subscription", subscriptionID)

	db := h.database
	// Process all channels that contains this feed
	rows, err := db.Query(`
select topic, c.uid, f.id, c.name
from subscriptions s
inner join feeds f    on f.url = s.topic
inner join channels c on c.id = f.channel_id
where s.id = $1
`,
		subscriptionID,
	)
	if err != nil {
		return err
	}
	// An unclosed rows holds a pooled connection until the process exits. One
	// per delivery walks the database up to max_connections, after which every
	// query fails with "too many clients already" and deliveries answer 400.
	defer rows.Close()

	b, err := ioutil.ReadAll(body)
	if err != nil {
		return err
	}

	buf := bytes.NewReader(b)

	for rows.Next() {
		var topic, channel, feedID, channelName string

		err = rows.Scan(&topic, &channel, &feedID, &channelName)
		if err != nil {
			slog.Error("websub: could not scan subscription row", "error", err)
			continue
		}

		slog.Info("websub: updating feed", "feed_id", feedID, "topic", topic, "channel_name", channelName)
		_, _, err = processor.ProcessContent(ctx, channel, feedID, topic, contentType, buf)
		if err != nil {
			slog.Error("websub: could not process content", "channel_name", channelName, "error", err)
		}

		// Reader#Seek never fails when whence exists
		_, _ = buf.Seek(0, io.SeekStart)
	}

	return rows.Err()
}

func (h *hubIncomingBackend) FeedSetLeaseSeconds(subscriptionID int64, leaseSeconds int64) error {
	db := h.database
	_, err := db.Exec(`
update subscriptions
set lease_seconds = $1,
    resubscribe_at = now() + $2 * interval '1' second
where id = $3
`, leaseSeconds, leaseSeconds, subscriptionID)
	return err
}

// FeedSetResubscribeAt moves the next renewal attempt, so a hub that refuses or
// rate limits the subscription is retried with a backoff instead of every tick.
func (h *hubIncomingBackend) FeedSetResubscribeAt(subscriptionID int64, at time.Time) error {
	db := h.database
	_, err := db.Exec(`update subscriptions set resubscribe_at = $1 where id = $2`, at, subscriptionID)
	return err
}

// Feeds returns a list of subscribed feeds
func (h *hubIncomingBackend) Feeds() ([]Feed, error) {
	db := h.database
	var feeds []Feed

	rows, err := db.Query(`
		select s.id, topic, hub, callback, subscription_secret, lease_seconds, resubscribe_at
		from subscriptions s
		inner join feeds f on f.url = s.topic
		inner join channels c on c.id = f.channel_id
		where hub is not null
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var feed Feed

		err = rows.Scan(
			&feed.ID,
			&feed.URL,
			&feed.Hub,
			&feed.Callback,
			&feed.Secret,
			&feed.LeaseSeconds,
			&feed.ResubscribeAt,
		)
		if err != nil {
			slog.Error("websub: could not list subscriptions", "error", err)
			continue
		}
		feeds = append(feeds, feed)
	}

	return feeds, rows.Err()
}

func (h *hubIncomingBackend) Subscribe(feed *Feed) error {
	slog.Debug("websub: subscribing", "feed", feed.URL)
	// A hub that accepts the connection but never answers would otherwise block
	// the whole renewal loop for every other feed.
	client := identifyingClient(30 * time.Second)
	return websub.Subscribe(client, feed.Hub, feed.URL, feed.Callback, feed.Secret, LeaseSeconds)
}

// subscribeBackoff gives the first retry after 15 minutes and doubles up to six
// hours, so a hub that rate limits or rejects us is not hammered every minute.
func subscribeBackoff(failures int) time.Duration {
	backoff := 15 * time.Minute
	for i := 1; i < failures && backoff < 6*time.Hour; i++ {
		backoff *= 2
	}
	if backoff > 6*time.Hour {
		backoff = 6 * time.Hour
	}
	return backoff
}

// retryHubDiscovery looks for subscriptions that never found a hub and tries
// again, with the channel's token when it has one.
func (h *hubIncomingBackend) retryHubDiscovery() {
	rows, err := h.database.Query(`
		select s.id, s.topic, coalesce(cs.settings->>'FeedToken', ''), coalesce(cs.settings->>'FeedMe', '')
		from subscriptions s
		inner join feeds f on f.url = s.topic
		inner join channels c on c.id = f.channel_id
		left join channel_settings cs on cs.channel_id = c.id
		where s.hub is null`)
	if err != nil {
		slog.Error("websub: could not list subscriptions without a hub", "error", errorString(err))
		return
	}
	type pending struct {
		id    int64
		topic string
		token string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		var me string
		if err := rows.Scan(&p.id, &p.topic, &p.token, &me); err != nil {
			continue
		}
		if p.token != "" && !strings.EqualFold(hostOfURL(p.topic), hostOfURL(me)) {
			p.token = ""
		}
		todo = append(todo, p)
	}
	rows.Close()

	for _, p := range todo {
		client := identifyingClient(0)
		if p.token != "" {
			client = tokenClient(p.token)
		}
		hubURL, err := websub.GetHubURL(client, p.topic)
		if err != nil || hubURL == "" {
			continue
		}
		callback := fmt.Sprintf("%s/incoming/%d", h.baseURL, p.id)
		if _, err := h.database.Exec(`UPDATE subscriptions SET hub = $1, callback = $2 WHERE id = $3`, hubURL, callback, p.id); err != nil {
			slog.Error("websub: could not save a discovered hub", "subscription", p.id, "error", errorString(err))
			continue
		}
		slog.Info("websub: hub found on retry", "topic", p.topic, "hub", hubURL)
	}
}

func (h *hubIncomingBackend) run() error {
	ticker := time.NewTicker(1 * time.Minute)
	quit := make(chan struct{})
	failures := map[int64]int{}

	go func() {
		for {
			select {
			case <-ticker.C:
				slog.Debug("websub: resubscribe pass started")
				varWebsub.Add("runs", 1)

				// A subscription with no hub is one whose discovery failed - a
				// private topic answers 401 to an anonymous read. Once the
				// channel holds a token those rows can find their hub, so they
				// are retried here instead of being left for a manual re-follow.
				h.retryHubDiscovery()

				feeds, err := h.Feeds()
				if err != nil {
					slog.Error("websub: could not list feeds", "error", errorString(err))
					slog.Debug("websub: resubscribe pass completed")
					continue
				}

				slog.Info("websub: feeds to check", "feeds", len(feeds))
				for _, feed := range feeds {
					slog.Debug("websub: looking at feed", "url", feed.URL)
					if feed.ResubscribeAt != nil && time.Now().Before(*feed.ResubscribeAt) {
						continue
					}
					if feed.Callback == "" {
						feed.Callback = fmt.Sprintf("%s/incoming/%d", h.baseURL, feed.ID)
					}
					slog.Info("websub: resubscribing",
						"feed_id", feed.ID, "topic", feed.URL, "hub", feed.Hub, "callback", feed.Callback)
					varWebsub.Add("resubscribe", 1)
					err := h.Subscribe(&feed)

					var next time.Time
					if err != nil {
						varWebsub.Add("errors", 1)
						failures[feed.ID]++
						backoff := subscribeBackoff(failures[feed.ID])
						next = time.Now().Add(backoff)
						slog.Warn("websub: subscription rejected",
							"feed_id", feed.ID,
							"topic", feed.URL,
							"hub", feed.Hub,
							"retry_in", backoff.String(),
							"error", errorString(err))
					} else {
						delete(failures, feed.ID)
						lease := time.Duration(feed.LeaseSeconds) * time.Second
						if lease <= 0 {
							lease = time.Duration(LeaseSeconds) * time.Second
						}
						next = time.Now().Add(lease / 2)
					}

					if e := h.FeedSetResubscribeAt(feed.ID, next); e != nil {
						slog.Error("websub: could not store resubscribe_at",
							"feed_id", feed.ID, "error", errorString(e))
					}
				}

				slog.Debug("websub: resubscribe pass completed")
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}
