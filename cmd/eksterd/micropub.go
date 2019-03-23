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
	"github.com/pkg/errors"
	"willnorris.com/go/microformats"
)

type micropubHandler struct {
	Backend *memoryBackend
	pool    *redis.Pool
}

func parseIncomingItem(r *http.Request) (*microsub.Item, error) {
	var item microsub.Item

	contentType := r.Header.Get("content-type")

	if contentType == "application/jf2+json" {
		dec := json.NewDecoder(r.Body)
		err := dec.Decode(&item)
		if err != nil {
			return nil, errors.Wrapf(err, "could not decode request body as jf2: %v", err)
		}
	} else if contentType == "application/json" {
		var mfItem microformats.Microformat
		dec := json.NewDecoder(r.Body)
		err := dec.Decode(&mfItem)
		if err != nil {
			return nil, errors.Wrapf(err, "could not decode request body as json: %v", err)
		}
		author := microsub.Card{}
		var ok bool
		item, ok = jf2.SimplifyMicroformatItem(&mfItem, author)
		if !ok {
			return nil, fmt.Errorf("could not simplify microformat item to jf2")
		}
	} else if contentType == "application/x-www-form-urlencoded" {
		content := r.FormValue("content")
		name := r.FormValue("name")
		item.Type = "entry"
		item.Name = name
		item.Content = &microsub.Content{Text: content}
		item.Published = time.Now().Format(time.RFC3339)
	} else {
		return nil, fmt.Errorf("content-type %s is not supported", contentType)
	}
	return &item, nil
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

	conn := h.pool.Get()
	defer conn.Close()

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	if r.Method == http.MethodPost {
		sourceID := r.URL.Query().Get("source_id")

		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			sourceID = authHeader[7:]
		}

		channel, err := redis.String(conn.Do("HGET", "sources", sourceID))
		if err != nil {
			channel, err = redis.String(conn.Do("HGET", "token:"+sourceID, "channel"))
			if err != nil {
				http.Error(w, "unauthorized", 401)
				return
			}
		}

		item, err := parseIncomingItem(r)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		item.Read = false
		id, _ := redis.Int(conn.Do("INCR", "source:"+sourceID+"next_id"))
		item.ID = fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("source:%s:%d", sourceID, id))))
		err = h.Backend.channelAddItemWithMatcher(channel, *item)
		err = h.Backend.updateChannelUnreadCount(channel)
		if err != nil {
			log.Printf("could not update channel unread content %s: %v", channel, err)
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
