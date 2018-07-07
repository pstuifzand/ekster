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
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

var (
	pool        *redis.Pool
	port        int
	auth        bool
	redisServer = flag.String("redis", "redis:6379", "")
	entryRegex  = regexp.MustCompile("^entry\\[\\d+\\]$")
)

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)

	flag.IntVar(&port, "port", 80, "port for serving api")
	flag.BoolVar(&auth, "auth", true, "use auth")
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

	if auth {
		log.Println("Using auth")
	} else {
		log.Println("Authentication disabled")
	}

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
