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
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/microsub-server/microsub"
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

type microsubHandler struct {
	Backend            microsub.Microsub
	HubIncomingBackend HubBackend
	Redis              redis.Conn
}

type hubIncomingBackend struct {
	backend *memoryBackend
	conn    redis.Conn
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Intn(len(letterBytes))]
	}
	return string(b)
}

func (h *hubIncomingBackend) GetSecret(id int64) string {
	secret, err := redis.String(h.conn.Do("HGET", fmt.Sprintf("feed:%d", id), "secret"))
	if err != nil {
		return ""
	}
	return secret
}

var hubURL = "https://hub.stuifzandapp.com/"

func (h *hubIncomingBackend) CreateFeed(topic string, channel string) (int64, error) {
	id, err := redis.Int64(h.conn.Do("INCR", "feed:next_id"))

	if err != nil {
		return 0, err
	}

	h.conn.Do("HSET", fmt.Sprintf("feed:%d", id), "url", topic)
	h.conn.Do("HSET", fmt.Sprintf("feed:%d", id), "channel", channel)
	secret := randStringBytes(16)
	h.conn.Do("HSET", fmt.Sprintf("feed:%d", id), "secret", secret)

	hub, err := url.Parse(hubURL)
	q := hub.Query()
	q.Add("hub.mode", "subscribe")
	q.Add("hub.callback", fmt.Sprintf("https://microsub.stuifzandapp.com/incoming/%d", id))
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
	u, err := redis.String(h.conn.Do("HGET", fmt.Sprintf("feed:%d", feedID), "url"))
	if err != nil {
		return err
	}
	channel, err := redis.String(h.conn.Do("HGET", fmt.Sprintf("feed:%d", feedID), "channel"))
	if err != nil {
		return err
	}

	h.backend.ProcessContent(channel, u, contentType, body)

	return err
}

func (h *microsubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var logger = log.New(os.Stdout, "logger: ", log.Lshortfile)
	h.Redis = redis.NewLoggingConn(pool.Get(), logger, "microsub")
	defer h.Redis.Close()

	r.ParseForm()
	log.Printf("%s %s\n", r.Method, r.URL)
	log.Println(r.URL.Query())
	log.Println(r.PostForm)
	authorization := r.Header.Get("Authorization")

	var token TokenResponse

	if !h.cachedCheckAuthToken(authorization, &token) {
		log.Printf("Token could not be validated")
		http.Error(w, "Can't validate token", 403)
		return
	}

	if token.Me != "https://publog.stuifzandapp.com/" {
		log.Printf("Missing \"me\" in token response: %#v\n", token)
		http.Error(w, "Wrong me", 403)
		return
	}

	if r.Method == http.MethodGet {
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			channels := h.Backend.ChannelsGetList()
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Channel{
				"channels": channels,
			})
		} else if action == "timeline" {
			timeline := h.Backend.TimelineGet(values.Get("after"), values.Get("before"), values.Get("channel"))
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.SetIndent("", "    ")
			jw.Encode(timeline)
		} else if action == "preview" {
			timeline := h.Backend.PreviewURL(values.Get("url"))
			jw := json.NewEncoder(w)
			jw.SetIndent("", "    ")
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(timeline)
		} else if action == "follow" {
			channel := values.Get("channel")
			following := h.Backend.FollowGetList(channel)
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Feed{
				"items": following,
			})
		} else {
			log.Printf("unknown action %s\n", action)
		}
		return
	} else if r.Method == http.MethodPost {
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			name := values.Get("name")
			method := values.Get("method")
			uid := values.Get("channel")
			if method == "delete" {
				h.Backend.ChannelsDelete(uid)
				w.Header().Add("Content-Type", "application/json")
				fmt.Fprintln(w, "[]")
				h.Backend.(Debug).Debug()
				return
			}

			jw := json.NewEncoder(w)
			if uid == "" {
				channel := h.Backend.ChannelsCreate(name)
				w.Header().Add("Content-Type", "application/json")
				jw.Encode(channel)
			} else {
				channel := h.Backend.ChannelsUpdate(uid, name)
				w.Header().Add("Content-Type", "application/json")
				jw.Encode(channel)
			}
			h.Backend.(Debug).Debug()
		} else if action == "follow" {
			uid := values.Get("channel")
			url := values.Get("url")
			h.HubIncomingBackend.CreateFeed(url, uid)
			feed := h.Backend.FollowURL(uid, url)
			w.Header().Add("Content-Type", "application/json")
			jw := json.NewEncoder(w)
			jw.Encode(feed)
		} else if action == "unfollow" {
			uid := values.Get("channel")
			url := values.Get("url")
			h.Backend.UnfollowURL(uid, url)
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else if action == "search" {
			query := values.Get("query")
			feeds := h.Backend.Search(query)
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.Encode(map[string][]microsub.Feed{
				"results": feeds,
			})
		} else if action == "timeline" || r.PostForm.Get("action") == "timeline" {
			method := values.Get("method")

			if method == "mark_read" || r.PostForm.Get("method") == "mark_read" {
				values = r.PostForm
				channel := values.Get("channel")
				if uids, e := values["entry"]; e {
					h.Backend.MarkRead(channel, uids)
				} else if uids, e := values["entry[]"]; e {
					h.Backend.MarkRead(channel, uids)
				} else {
					uids := []string{}
					for k, v := range values {
						if entryRegex.MatchString(k) {
							uids = append(uids, v...)
						}
					}
					h.Backend.MarkRead(channel, uids)
				}
			} else {
				log.Printf("unknown method in timeline %s\n", method)
			}
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else {
			log.Printf("unknown action %s\n", action)
		}

		return
	}
	return
}

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

func main() {
	flag.Parse()

	var logger = log.New(os.Stdout, "logger: ", log.Lshortfile)

	createBackend := false
	args := flag.Args()

	if len(args) >= 1 {
		if args[0] == "new" {
			createBackend = true
		}
	}

	pool = newPool(*redisServer)

	conn := redis.NewLoggingConn(pool.Get(), logger, "microsub")
	defer conn.Close()

	var backend microsub.Microsub

	if createBackend {
		backend = createMemoryBackend()
		return
	}

	backend = loadMemoryBackend(conn)

	hubBackend := hubIncomingBackend{backend.(*memoryBackend), conn}

	http.Handle("/microsub", &microsubHandler{
		Backend:            backend,
		HubIncomingBackend: &hubBackend,
		Redis:              nil,
	})
	http.Handle("/incoming/", &incomingHandler{
		Backend: &hubBackend,
	})
	backend.(*memoryBackend).run()
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
