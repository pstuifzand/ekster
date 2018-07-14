package main

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
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

			item = mapToItem(simplifyMicroformat(&mfItem))
			ok = true
		} else {
			http.Error(w, "Unsupported Content-Type", 400)
			return
		}

		if ok {
			item.Read = false
			id, _ := redis.Int(conn.Do("INCR", "source:"+sourceID+"next_id"))
			item.ID = fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("source:%s:%d", sourceID, id))))
			h.Backend.channelAddItem(conn, channel, item)
			err = h.Backend.updateChannelUnreadCount(conn, channel)
			if err != nil {
				log.Printf("error: while updating channel unread count for %s: %s\n", channel, err)
			}
		}

		w.Header().Set("Content-Type", "application/json")

		enc := json.NewEncoder(w)
		enc.Encode(map[string]string{
			"ok": "1",
		})

		return
	}

	http.Error(w, "Method not allowed", 405)
}
