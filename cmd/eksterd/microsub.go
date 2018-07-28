package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
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
			channels, err := h.Backend.ChannelsGetList()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			err = jw.Encode(map[string][]microsub.Channel{
				"channels": channels,
			})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else if action == "timeline" {
			timeline, err := h.Backend.TimelineGet(values.Get("before"), values.Get("after"), values.Get("channel"))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			jw.SetIndent("", "    ")
			jw.SetEscapeHTML(false)
			err = jw.Encode(timeline)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else if action == "preview" {
			timeline, err := h.Backend.PreviewURL(values.Get("url"))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jw := json.NewEncoder(w)
			jw.SetIndent("", "    ")
			w.Header().Add("Content-Type", "application/json")
			err = jw.Encode(timeline)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else if action == "follow" {
			channel := values.Get("channel")
			following, err := h.Backend.FollowGetList(channel)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			err = jw.Encode(map[string][]microsub.Feed{
				"items": following,
			})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else {
			http.Error(w, fmt.Sprintf("unknown action %s\n", action), 500)
			return
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
				err := h.Backend.ChannelsDelete(uid)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Add("Content-Type", "application/json")
				fmt.Fprintln(w, "[]")
				h.Backend.(Debug).Debug()
				return
			}

			jw := json.NewEncoder(w)
			if uid == "" {
				channel, err := h.Backend.ChannelsCreate(name)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Add("Content-Type", "application/json")
				err = jw.Encode(channel)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			} else {
				channel, err := h.Backend.ChannelsUpdate(uid, name)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				w.Header().Add("Content-Type", "application/json")
				err = jw.Encode(channel)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
			}
			h.Backend.(Debug).Debug()
		} else if action == "follow" {
			uid := values.Get("channel")
			url := values.Get("url")
			h.HubIncomingBackend.CreateFeed(url, uid)
			feed, err := h.Backend.FollowURL(uid, url)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Add("Content-Type", "application/json")
			jw := json.NewEncoder(w)
			err = jw.Encode(feed)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else if action == "unfollow" {
			uid := values.Get("channel")
			url := values.Get("url")
			err := h.Backend.UnfollowURL(uid, url)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else if action == "search" {
			query := values.Get("query")
			feeds, err := h.Backend.Search(query)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			jw := json.NewEncoder(w)
			w.Header().Add("Content-Type", "application/json")
			err = jw.Encode(map[string][]microsub.Feed{
				"results": feeds,
			})
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
		} else if action == "timeline" || r.PostForm.Get("action") == "timeline" {
			method := values.Get("method")

			if method == "mark_read" || r.PostForm.Get("method") == "mark_read" {
				values = r.Form
				channel := values.Get("channel")
				if uids, e := values["entry"]; e {
					err := h.Backend.MarkRead(channel, uids)
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				} else if uids, e := values["entry[]"]; e {
					err := h.Backend.MarkRead(channel, uids)
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				} else {
					uids := []string{}
					for k, v := range values {
						if entryRegex.MatchString(k) {
							uids = append(uids, v...)
						}
					}
					err := h.Backend.MarkRead(channel, uids)
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				}
			} else {
				http.Error(w, fmt.Sprintf("unknown method in timeline %s\n", method), 500)
				return
			}
			w.Header().Add("Content-Type", "application/json")
			fmt.Fprintln(w, "[]")
		} else {
			http.Error(w, fmt.Sprintf("unknown action %s\n", action), 500)
		}

		return
	}
	return
}
