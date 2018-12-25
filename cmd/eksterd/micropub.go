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
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"p83.nl/go/ekster/pkg/jf2"
	"p83.nl/go/ekster/pkg/microsub"

	"github.com/gomodule/redigo/redis"
	"willnorris.com/go/microformats"
)

type micropubHandler struct {
	Backend *memoryBackend
}

/*
 * URLs needed:
 * - /		      with endpoint urls
 * - /micropub    micropub endpoint
 * - /auth        auth endpoint
 * - /token       token endpoint
 */
func (h *micropubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	conn := pool.Get()
	defer conn.Close()

	r.ParseForm()

	if r.Method == http.MethodGet {
		// show profile with endpoint urls

	} else if r.Method == http.MethodPost {
		sourceID := r.URL.Query().Get("source_id")

		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			sourceID = authHeader[7:]
		}

		channel, err := redis.String(conn.Do("HGET", "sources", sourceID))
		if err != nil {

			channel, err = redis.String(conn.Do("HGET", "token:"+sourceID, "channel"))
			if err != nil {
				http.Error(w, "Unknown source", 400)
				return
			}
		}

		var item microsub.Item
		ok := false
		if r.Header.Get("Content-Type") == "application/jf2+json" {
			dec := json.NewDecoder(r.Body)
			err := dec.Decode(&item)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error decoding: %v", err), 400)
				return
			}
			ok = true
		} else if r.Header.Get("Content-Type") == "application/json" {
			var mfItem microformats.Microformat
			dec := json.NewDecoder(r.Body)
			err := dec.Decode(&mfItem)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error decoding: %v", err), 400)
				return
			}

			author := microsub.Card{}
			item, ok = jf2.SimplifyMicroformatItem(&mfItem, author)
		} else if r.Header.Get("Content-Type") == "application/x-www-form-urlencoded" {
			content := r.FormValue("content")
			name := r.FormValue("name")
			item.Type = "entry"
			item.Name = name
			item.Content = &microsub.Content{Text: content}
			item.Published = time.Now().Format(time.RFC3339)
			ok = true
		} else {
			http.Error(w, "Unsupported Content-Type", 400)
			return
		}

		if ok {
			item.Read = false
			id, _ := redis.Int(conn.Do("INCR", "source:"+sourceID+"next_id"))
			item.ID = fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("source:%s:%d", sourceID, id))))
			err = h.Backend.channelAddItemWithMatcher(channel, item)
			err = h.Backend.updateChannelUnreadCount(channel)
			if err != nil {
				log.Printf("error: while updating channel unread count for %s: %s\n", channel, err)
			}
		}

		w.Header().Set("Content-Type", "application/json")

		enc := json.NewEncoder(w)
		err = enc.Encode(map[string]string{
			"ok": "1",
		})

		return
	}

	http.Error(w, "Method not allowed", 405)
}
