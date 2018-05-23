package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/microsub"
)

type microsubHandler struct {
	Backend            microsub.Microsub
	HubIncomingBackend HubBackend
	Redis              redis.Conn
}

func (h *microsubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var logger = log.New(os.Stdout, "logger: ", log.Lshortfile)
	h.Redis = redis.NewLoggingConn(pool.Get(), logger, "microsub")
	defer h.Redis.Close()

	r.ParseForm()
	log.Printf("%s %s\n", r.Method, r.URL)
	log.Println(r.URL.Query())
	log.Println(r.PostForm)

	if auth {
		authorization := r.Header.Get("Authorization")

		var token TokenResponse

		if !h.cachedCheckAuthToken(authorization, &token) {
			log.Printf("Token could not be validated")
			http.Error(w, "Can't validate token", 403)
			return
		}

		if token.Me != h.Backend.(*memoryBackend).Me {
			log.Printf("Missing \"me\" in token response: %#v\n", token)
			http.Error(w, "Wrong me", 403)
			return
		}
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
			timeline := h.Backend.TimelineGet(values.Get("before"), values.Get("after"), values.Get("channel"))
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
				values = r.Form
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
