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
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"linkheader"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/microsub"
	"github.com/pstuifzand/ekster/pkg/util"
)

var (
	pool        *redis.Pool
	port        int
	redisServer = flag.String("redis", "redis:6379", "")
	entryRegex  = regexp.MustCompile("^entry\\[\\d+\\]$")
)

func init() {
	flag.IntVar(&port, "port", 80, "port for serving api")
}

type mainHandler struct {
	Backend *memoryBackend
}

func (h *mainHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		fmt.Fprintln(w, "<h1>Ekster - Microsub server</h1>")
		fmt.Fprintln(w, `<p><a href="/settings">Settings</a></p>`)
		return
	}
	http.NotFound(w, r)
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

func (h *hubIncomingBackend) getHubURL(topic string) (string, error) {
	client := &http.Client{}

	resp, err := client.Head(topic)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if headers, e := resp.Header["Link"]; e {
		links := linkheader.ParseMultiple(headers)
		for _, link := range links {
			if link.Rel == "hub" {
				log.Printf("WebSub Hub URL found for topic=%s hub=%s\n", topic, link.URL)
				return link.URL, nil
			}
		}
	}

	log.Printf("WebSub Hub URL not found for topic=%s\n", topic)
	return "", nil
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

	hubURL, err := h.getHubURL(topic)
	if err == nil && hubURL != "" {
		conn.Do("HSET", fmt.Sprintf("feed:%d", id), "hub", hubURL)
	} else {
		return id, nil
	}

	hub, err := url.Parse(hubURL)
	q := hub.Query()
	q.Add("hub.mode", "subscribe")
	q.Add("hub.callback", fmt.Sprintf("%s/incoming/%d", os.Getenv("EKSTER_BASEURL"), id))
	q.Add("hub.topic", topic)
	q.Add("hub.secret", secret)
	hub.RawQuery = ""

	log.Printf("POST %s\n", hub)
	client := &http.Client{}
	res, err := client.PostForm(hub.String(), q)
	if err != nil {
		log.Printf("new request: %s\n", err)
		return 0, err
	}
	defer res.Body.Close()

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

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

func main() {
	log.Println("eksterd - microsub server")
	flag.Parse()

	if _, e := os.LookupEnv("EKSTER_BASEURL"); !e {
		log.Fatal("EKSTER_BASEURL environment variable not found, please set with external url: https://example.com")
	}

	createBackend := false
	args := flag.Args()

	if len(args) >= 1 {
		if args[0] == "new" {
			createBackend = true
		}
	}

	pool = newPool(*redisServer)

	var backend microsub.Microsub

	if createBackend {
		backend = createMemoryBackend()
		return
	}

	backend = loadMemoryBackend()

	hubBackend := hubIncomingBackend{backend.(*memoryBackend)}

	http.Handle("/micropub", &micropubHandler{
		Backend: backend.(*memoryBackend),
	})

	http.Handle("/microsub", &microsubHandler{
		Backend:            backend,
		HubIncomingBackend: &hubBackend,
		Redis:              nil,
	})
	http.Handle("/incoming/", &incomingHandler{
		Backend: &hubBackend,
	})

	http.Handle("/", &mainHandler{
		Backend: backend.(*memoryBackend),
	})

	backend.(*memoryBackend).run()
	log.Printf("Listening on port %d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
