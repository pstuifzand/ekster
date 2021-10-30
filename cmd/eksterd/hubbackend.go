package main

import (
	"expvar"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	"p83.nl/go/ekster/pkg/util"
	"p83.nl/go/ekster/pkg/websub"

	"github.com/gomodule/redigo/redis"
)

// LeaseSeconds is the default number of seconds we want the subscription to last
const LeaseSeconds = 24 * 60 * 60

// HubBackend handles information for the incoming handler
type HubBackend interface {
	GetFeeds() []Feed // Deprecated
	Feeds() ([]Feed, error)
	CreateFeed(url, channel string) (int64, error)
	GetSecret(feedID int64) string
	UpdateFeed(feedID int64, contentType string, body io.Reader) error
	FeedSetLeaseSeconds(feedID int64, leaseSeconds int64) error
	Subscribe(feed *Feed) error
}

type hubIncomingBackend struct {
	backend *memoryBackend
	baseURL string
	pool    *redis.Pool
}

// Feed contains information about the feed subscriptions
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

var (
	varWebsub *expvar.Map
)

func init() {
	varWebsub = expvar.NewMap("websub")
}

func (h *hubIncomingBackend) GetSecret(id int64) string {
	conn := h.pool.Get()
	defer conn.Close()
	secret, err := redis.String(conn.Do("HGET", fmt.Sprintf("feed:%d", id), "secret"))
	if err != nil {
		return ""
	}
	return secret
}

func (h *hubIncomingBackend) CreateFeed(topic string, channel string) (int64, error) {
	conn := h.pool.Get()
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

	callbackURL := fmt.Sprintf("%s/incoming/%d", h.baseURL, id)

	log.Printf("WebSub Hub URL found for topic=%q hub=%q callback=%q\n", topic, hubURL, callbackURL)

	if err == nil && hubURL != "" {
		args := redis.Args{}.Add(fmt.Sprintf("feed:%d", id), "hub", hubURL, "callback", callbackURL)
		_, err = conn.Do("HMSET", args...)
		if err != nil {
			return 0, errors.Wrap(err, "could not write to redis backend")
		}
	} else {
		return id, nil
	}

	err = websub.Subscribe(client, hubURL, topic, callbackURL, secret, 24*3600)
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (h *hubIncomingBackend) UpdateFeed(feedID int64, contentType string, body io.Reader) error {
	conn := h.pool.Get()
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

	// FIXME: feed id for incoming websub content
	log.Printf("Updating feed %d - %s %s\n", feedID, u, channel)
	err = h.backend.ProcessContent(channel, fmt.Sprintf("incoming:%d", feedID), u, contentType, body)
	if err != nil {
		log.Printf("could not process content for channel %s: %s", channel, err)
	}

	return err
}

func (h *hubIncomingBackend) FeedSetLeaseSeconds(feedID int64, leaseSeconds int64) error {
	conn := h.pool.Get()
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

// GetFeeds is deprecated, use Feeds instead
func (h *hubIncomingBackend) GetFeeds() []Feed {
	log.Println("GetFeeds called, consider replacing with Feeds")
	feeds, err := h.Feeds()
	if err != nil {
		log.Printf("Feeds returned an error: %v", err)
	}
	return feeds
}

// Feeds returns a list of subscribed feeds
func (h *hubIncomingBackend) Feeds() ([]Feed, error) {
	conn := h.pool.Get()
	defer conn.Close()
	feeds := []Feed{}

	// FIXME(peter): replace with set of currently checked feeds
	feedKeys, err := redis.Strings(conn.Do("KEYS", "feed:*"))
	if err != nil {
		return nil, errors.Wrap(err, "could not get feeds from backend")
	}

	for _, feedKey := range feedKeys {
		var feed Feed
		values, err := redis.Values(conn.Do("HGETALL", feedKey))
		if err != nil {
			log.Printf("could not get feed info for key %s: %v", feedKey, err)
			continue
		}

		err = redis.ScanStruct(values, &feed)
		if err != nil {
			log.Printf("could not scan struct for key %s: %v", feedKey, err)
			continue
		}

		// Add feed id
		if feed.ID == 0 {
			parts := strings.Split(feedKey, ":")
			if len(parts) == 2 {
				feed.ID, _ = strconv.ParseInt(parts[1], 10, 64)
				_, err = conn.Do("HSET", feedKey, "id", feed.ID)
				if err != nil {
					log.Printf("could not save id for %s: %v", feedKey, err)
				}
			}
		}

		// Fix the callback url
		callbackURL, err := url.Parse(feed.Callback)
		if err != nil || !callbackURL.IsAbs() {
			if err != nil {
				log.Printf("could not parse callback url %q: %v", callbackURL, err)
			} else {
				log.Printf("url is relative; replace with absolute url: %q", callbackURL)
			}

			feed.Callback = fmt.Sprintf("%s/incoming/%d", h.baseURL, feed.ID)
			_, err = conn.Do("HSET", feedKey, "callback", feed.Callback)
			if err != nil {
				log.Printf("could not save id for %s: %v", feedKey, err)
			}
		}

		// Skip feeds without a Hub
		if feed.Hub == "" {
			continue
		}

		log.Printf("Websub feed: %#v\n", feed)
		feeds = append(feeds, feed)
	}

	return feeds, nil
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
				varWebsub.Add("runs", 1)

				feeds, err := h.Feeds()
				if err != nil {
				}

				for _, feed := range feeds {
					log.Printf("Looking at %s\n", feed.URL)
					if feed.ResubscribeAt == 0 || time.Now().After(time.Unix(feed.ResubscribeAt, 0)) {
						if feed.Callback == "" {
							feed.Callback = fmt.Sprintf("%s/incoming/%d", h.baseURL, feed.ID)
						}
						log.Printf("Send resubscribe for %q on %q with callback %q\n", feed.URL, feed.Hub, feed.Callback)
						varWebsub.Add("resubscribe", 1)
						err := h.Subscribe(&feed)
						if err != nil {
							log.Printf("Error while subscribing: %s", err)
							varWebsub.Add("errors", 1)
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
