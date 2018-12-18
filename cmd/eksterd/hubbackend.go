/*
   ekster - microsub server
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
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"p83.nl/go/ekster/pkg/util"
	"p83.nl/go/ekster/pkg/websub"

	"github.com/gomodule/redigo/redis"
)

// LeaseSeconds is the default number of seconds we want the subscription to last
const LeaseSeconds = 24 * 60 * 60

// HubBackend handles information for the incoming handler
type HubBackend interface {
	GetFeeds() []Feed
	CreateFeed(url, channel string) (int64, error)
	GetSecret(feedID int64) string
	UpdateFeed(feedID int64, contentType string, body io.Reader) error
	FeedSetLeaseSeconds(feedID int64, leaseSeconds int64) error
	Subscribe(feed *Feed) error
}

type hubIncomingBackend struct {
	backend *memoryBackend
	baseURL string
}

type Feed struct {
	ID            int64  `redis:"id"`
	Channel       string `redis:"channel"`
	URL           string `redis:"url"`
	Callback      string `redis:"callback"`
	Hub           string `redis:"hub"`
	Secret        string `redis:"secret"`
	LeaseSeconds  int64  `redis:"lease_seconds"`
	ResubscribeAt int64  `redis:"resubscribe_at"`
}

func (h *hubIncomingBackend) GetSecret(id int64) string {
	conn := pool.Get()
	defer conn.Close()
	secret, err := redis.String(conn.Do("HGET", fmt.Sprintf("feed:%d", id), "secret"))
	if err != nil {
		return ""
	}
	return secret
}

func (h *hubIncomingBackend) CreateFeed(topic string, channel string) (int64, error) {
	conn := pool.Get()
	defer conn.Close()

	// TODO(peter): check if topic already is registered
	id, err := redis.Int64(conn.Do("INCR", "feed:next_id"))
	if err != nil {
		return 0, err
	}

	conn.Do("HSET", fmt.Sprintf("feed:%d", id), "url", topic)
	conn.Do("HSET", fmt.Sprintf("feed:%d", id), "channel", channel)
	secret := util.RandStringBytes(16)
	conn.Do("HSET", fmt.Sprintf("feed:%d", id), "secret", secret)

	client := &http.Client{}

	hubURL, err := websub.GetHubURL(client, topic)
	if err != nil {
		log.Printf("WebSub Hub URL not found for topic=%s\n", topic)
		return 0, err
	}

	log.Printf("WebSub Hub URL found for topic=%s hub=%s\n", topic, hubURL)

	callbackURL := fmt.Sprintf("%s/incoming/%d", h.baseURL, id)

	if err == nil && hubURL != "" {
		args := redis.Args{}.Add(fmt.Sprintf("feed:%d", id), "hub", hubURL, "callback", callbackURL)
		conn.Do("HMSET", args...)
	} else {
		return id, nil
	}

	websub.Subscribe(client, hubURL, topic, callbackURL, secret, 24*3600)

	return id, nil
}

func (h *hubIncomingBackend) UpdateFeed(feedID int64, contentType string, body io.Reader) error {
	conn := pool.Get()
	defer conn.Close()
	log.Printf("updating feed %d", feedID)
	u, err := redis.String(conn.Do("HGET", fmt.Sprintf("feed:%d", feedID), "url"))
	if err != nil {
		return err
	}
	channel, err := redis.String(conn.Do("HGET", fmt.Sprintf("feed:%d", feedID), "channel"))
	if err != nil {
		return err
	}

	log.Printf("Updating feed %d - %s %s\n", feedID, u, channel)
	err = h.backend.ProcessContent(channel, u, contentType, body)
	if err != nil {
		log.Printf("Error while updating content for channel %s: %s", channel, err)
	}

	return err
}

func (h *hubIncomingBackend) FeedSetLeaseSeconds(feedID int64, leaseSeconds int64) error {
	conn := pool.Get()
	defer conn.Close()
	log.Printf("updating feed %d lease_seconds", feedID)

	args := redis.Args{}.Add(fmt.Sprintf("feed:%d", feedID), "lease_seconds", leaseSeconds, "resubscribe_at", time.Now().Add(time.Duration(60*(leaseSeconds-15))*time.Second).Unix())
	_, err := conn.Do("HMSET", args...)
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

func (h *hubIncomingBackend) GetFeeds() []Feed {
	conn := pool.Get()
	defer conn.Close()
	feeds := []Feed{}

	// FIXME(peter): replace with set of currently checked feeds
	feedKeys, err := redis.Strings(conn.Do("KEYS", "feed:*"))
	if err != nil {
		log.Println(err)
		return feeds
	}

	for _, feedKey := range feedKeys {
		var feed Feed
		values, err := redis.Values(conn.Do("HGETALL", feedKey))
		if err != nil {
			log.Println(err)
			continue
		}

		err = redis.ScanStruct(values, &feed)
		if err != nil {
			log.Println(err)
			continue
		}

		if feed.ID == 0 {
			parts := strings.Split(feedKey, ":")
			if len(parts) == 2 {
				feed.ID, _ = strconv.ParseInt(parts[1], 10, 64)
				conn.Do("HPUT", feedKey, "id", feed.ID)
			}
		}

		// Skip feeds without a Hub
		if feed.Hub == "" {
			continue
		}

		log.Printf("Websub feed: %#v\n", feed)
		feeds = append(feeds, feed)
	}

	return feeds
}

func (h *hubIncomingBackend) Subscribe(feed *Feed) error {
	client := http.Client{}
	return websub.Subscribe(&client, feed.Hub, feed.URL, feed.Callback, feed.Secret, LeaseSeconds)
}

func (h *hubIncomingBackend) run() error {
	ticker := time.NewTicker(10 * time.Minute)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				log.Println("Getting feeds for WebSub")
				feeds := h.GetFeeds()
				for _, feed := range feeds {
					log.Printf("Looking at %s\n", feed.URL)
					if feed.ResubscribeAt == 0 || time.Now().After(time.Unix(feed.ResubscribeAt, 0)) {
						if feed.Callback == "" {
							feed.Callback = fmt.Sprintf("%s/incoming/%d", h.baseURL, feed.ID)
						}
						log.Printf("Send resubscribe for %q on %q\n", feed.URL, feed.Hub)
						err := h.Subscribe(&feed)
						if err != nil {
							log.Printf("Error while subscribing: %s", err)
						}
					}
				}
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}
