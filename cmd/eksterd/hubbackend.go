package main

import (
	"database/sql"
	"expvar"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"p83.nl/go/ekster/pkg/util"
	"p83.nl/go/ekster/pkg/websub"

	"github.com/gomodule/redigo/redis"
)

// LeaseSeconds is the default number of seconds we want the subscription to last
const LeaseSeconds = 24 * 60 * 60

// HubBackend handles information for the incoming handler
type HubBackend interface {
	Feeds() ([]Feed, error)
	CreateFeed(url string) (int64, error)
	GetSecret(feedID int64) string
	UpdateFeed(processor ContentProcessor, feedID int64, contentType string, body io.Reader) error
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
	ResubscribeAt time.Time
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

func (h *hubIncomingBackend) CreateFeed(topic string) (int64, error) {
	log.Println("CreateFeed", topic)
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

	client := &http.Client{}

	hubURL, err := websub.GetHubURL(client, topic)
	if err != nil {
		log.Printf("WebSub Hub URL not found for topic=%s\n", topic)
		return 0, err
	}

	callbackURL := fmt.Sprintf("%s/incoming/%d", h.baseURL, subscriptionID)

	log.Printf("WebSub Hub URL found for topic=%q hub=%q callback=%q\n", topic, hubURL, callbackURL)

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

func (h *hubIncomingBackend) UpdateFeed(processor ContentProcessor, subscriptionID int64, contentType string, body io.Reader) error {
	log.Println("UpdateFeed", subscriptionID)

	db := h.database
	var (
		topic   string
		channel string
		feedID  string
	)

	// Process all channels that contains this feed
	rows, err := db.Query(
		`select topic, c.uid, f.id from subscriptions s inner join feeds f on f.url = s.topic inner join channels c on c.id = f.channel_id where s.id = $1`,
		subscriptionID,
	)
	if err != nil {
		return err
	}

	for rows.Next() {
		err = rows.Scan(&topic, &channel, &feedID)
		if err != nil {
			log.Println(err)
			continue
		}

		log.Printf("Updating feed %s %q in %q\n", feedID, topic, channel)
		err = processor.ProcessContent(channel, feedID, topic, contentType, body)
		if err != nil {
			log.Printf("could not process content for channel %s: %s", channel, err)
		}
	}

	return err
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
			log.Println("Feeds: scan subscriptions:", err)
			continue
		}
		feeds = append(feeds, feed)
	}

	return feeds, nil
}

func (h *hubIncomingBackend) Subscribe(feed *Feed) error {
	log.Println("Subscribe", feed.URL)
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
					log.Println("Feeds failed:", err)
					continue
				}

				for _, feed := range feeds {
					log.Printf("Looking at %s\n", feed.URL)
					if time.Now().After(feed.ResubscribeAt) {
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
