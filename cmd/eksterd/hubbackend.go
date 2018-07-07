package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/util"
	"github.com/pstuifzand/ekster/pkg/websub"
)

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

	args := redis.Args{}.Add(fmt.Sprintf("feed:%d", feedID), "lease_seconds", leaseSeconds)
	conn.Do("HSET", args...)

	return nil
}

func (h *hubIncomingBackend) run() error {
	ticker := time.NewTicker(10 * time.Minute)
	quit := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
			case <-quit:
				ticker.Stop()
				return
			}
		}
	}()

	return nil
}
