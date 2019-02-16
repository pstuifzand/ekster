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
package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"

	"p83.nl/go/ekster/pkg/microsub"
)

var (
	entryRegex = regexp.MustCompile("^entry\\[\\d+\\]$")
)

const (
	OutputContentType = "application/json; charset=utf-8"
)

type microsubHandler struct {
	backend microsub.Microsub
}

func respondJSON(w http.ResponseWriter, value interface{}) {
	jw := json.NewEncoder(w)
	jw.SetIndent("", "    ")
	jw.SetEscapeHTML(false)
	w.Header().Add("Content-Type", OutputContentType)
	err := jw.Encode(value)
	if err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func NewMicrosubHandler(backend microsub.Microsub) http.Handler {
	return &microsubHandler{backend}
}

func (h *microsubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()

	log.Printf("Incoming request: %s %s\n", r.Method, r.URL)
	log.Println(r.URL.Query())
	log.Println(r.PostForm)

	if r.Method == http.MethodOptions {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		w.Header().Add("Access-Control-Allow-Methods", "GET, POST")
		w.Header().Add("Access-Control-Allow-Headers", "Authorization, Cache-Control")
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		values := r.URL.Query()
		action := values.Get("action")
		if action == "channels" {
			channels, err := h.backend.ChannelsGetList()
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, map[string][]microsub.Channel{
				"channels": channels,
			})
		} else if action == "timeline" {
			timeline, err := h.backend.TimelineGet(values.Get("before"), values.Get("after"), values.Get("channel"))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, timeline)
		} else if action == "preview" {
			timeline, err := h.backend.PreviewURL(values.Get("url"))
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, timeline)
		} else if action == "follow" {
			channel := values.Get("channel")
			following, err := h.backend.FollowGetList(channel)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, map[string][]microsub.Feed{
				"items": following,
			})
		} else if action == "events" {
			conn, _, _ := w.(http.Hijacker).Hijack()
			cons := newConsumer(conn)
			h.backend.AddEventListener(cons)
		} else {
			http.Error(w, fmt.Sprintf("unknown action %s\n", action), 400)
			return
		}
		return
	} else if r.Method == http.MethodPost {
		w.Header().Add("Access-Control-Allow-Origin", "*")

		values := r.Form
		action := values.Get("action")
		if action == "channels" {
			name := values.Get("name")
			method := values.Get("method")
			uid := values.Get("channel")
			if method == "delete" {
				err := h.backend.ChannelsDelete(uid)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				respondJSON(w, []string{})
				return
			}

			if uid == "" {
				channel, err := h.backend.ChannelsCreate(name)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				respondJSON(w, channel)
			} else {
				channel, err := h.backend.ChannelsUpdate(uid, name)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				respondJSON(w, channel)
			}
		} else if action == "follow" {
			uid := values.Get("channel")
			url := values.Get("url")
			// h.HubIncomingBackend.CreateFeed(url, uid)
			feed, err := h.backend.FollowURL(uid, url)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, feed)
		} else if action == "unfollow" {
			uid := values.Get("channel")
			url := values.Get("url")
			err := h.backend.UnfollowURL(uid, url)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, []string{})
		} else if action == "search" {
			query := values.Get("query")
			feeds, err := h.backend.Search(query)
			if err != nil {
				http.Error(w, err.Error(), 500)
				return
			}
			respondJSON(w, map[string][]microsub.Feed{
				"results": feeds,
			})
		} else if action == "timeline" || r.PostForm.Get("action") == "timeline" {
			method := values.Get("method")

			if method == "mark_read" || r.PostForm.Get("method") == "mark_read" {
				values = r.Form
				channel := values.Get("channel")
				var markAsRead []string
				if uids, e := values["entry"]; e {
					markAsRead = uids
				} else if uids, e := values["entry[]"]; e {
					markAsRead = uids
				} else {
					uids := []string{}
					for k, v := range values {
						if entryRegex.MatchString(k) {
							uids = append(uids, v...)
						}
					}
					markAsRead = uids
				}

				if len(markAsRead) > 0 {
					err := h.backend.MarkRead(channel, markAsRead)
					if err != nil {
						http.Error(w, err.Error(), 500)
						return
					}
				}
			} else {
				http.Error(w, fmt.Sprintf("unknown method in timeline %s\n", method), 500)
				return
			}

			respondJSON(w, []string{})
		} else {
			http.Error(w, fmt.Sprintf("unknown action %s\n", action), 400)
		}
		return
	}
	return
}
