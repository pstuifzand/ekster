package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/util"
	"github.com/pstuifzand/ekster/pkg/websub"
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
	if hubURL == "" {
		log.Printf("WebSub Hub URL not found for topic=%s\n", topic)
	} else {
		log.Printf("WebSub Hub URL found for topic=%s hub=%s\n", topic, hubURL)
	}

	callbackURL := fmt.Sprintf("%s/incoming/%d", os.Getenv("EKSTER_BASEURL"), id)

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

	log.Printf("updating feed %d - %s %s\n", feedID, u, channel)
	h.backend.ProcessContent(channel, u, contentType, body)

	return err
}

func (h *hubIncomingBackend) FeedSetLeaseSeconds(feedID int64, leaseSeconds int64) error {
	conn := pool.Get()
	defer conn.Close()
	log.Printf("updating feed %d lease_seconds", feedID)

	args := redis.Args{}.Add(fmt.Sprintf("feed:%d", feedID), "lease_seconds", leaseSeconds, "resubscribe_at", time.Now().Add(time.Duration(60*(leaseSeconds-15))*time.Second))
	_, err := conn.Do("HMSET", args...)
	if err != nil {
		log.Println(err)
		return err
	}

	return nil
}

type Feed struct {
	ID            int64      `redis:"id"`
	Channel       string     `redis:"channel"`
	URL           string     `redis:"url"`
	Callback      string     `redis:"callback"`
	Hub           string     `redis:"hub"`
	Secret        string     `redis:"secret"`
	LeaseSeconds  int64      `redis:"lease_seconds"`
	ResubscribeAt *time.Time `redis:"resubscribe_at"`
}

func (h *hubIncomingBackend) GetFeeds() []Feed {
	conn := pool.Get()
	defer conn.Close()
	feeds := []Feed{}

	feedKeys, err := redis.Strings(conn.Do("KEYS feed:*"))
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
				feeds := h.GetFeeds()
				for _, feed := range feeds {
					if feed.ResubscribeAt == nil || time.Now().After(*feed.ResubscribeAt) {
						if feed.Callback == "" {
							feed.Callback = fmt.Sprintf("%s/incoming/%d", os.Getenv("EKSTER_BASEURL"), feed.ID)
						}
						h.Subscribe(&feed)
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
