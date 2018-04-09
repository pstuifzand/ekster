package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/microsub-server/microsub"
)

type micropubHandler struct {
	Backend *memoryBackend
}

func (h *micropubHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	conn := pool.Get()
	defer conn.Close()

	if r.Method == http.MethodPost {
		sourceID := r.URL.Query().Get("source_id")

		channel, err := redis.String(conn.Do("HGET", "sources", sourceID))
		if err != nil {
			http.Error(w, "Unknown source", 400)
			return
		}

		if r.Header.Get("Content-Type") == "application/jf2+json" {
			var item microsub.Item

			dec := json.NewDecoder(r.Body)

			err := dec.Decode(&item)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error decoding: %v", err), 400)
				return
			}

			item.Read = false
			item.ID = hex.EncodeToString([]byte(item.URL))

			h.Backend.channelAddItem(channel, item)
		} else {
			http.Error(w, "Unsupported Content-Type", 400)
			return
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
