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
	"time"

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/auth"

	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/server"
)

const (
	ClientID string = "https://p83.nl/microsub-client"
)

var (
	pool        *redis.Pool
	port        int
	authEnabled bool
	redisServer = flag.String("redis", "redis:6379", "")
)

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)

	flag.IntVar(&port, "port", 80, "port for serving api")
	flag.BoolVar(&authEnabled, "auth", true, "use auth")
}

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

func WithAuth(handler http.Handler, b *memoryBackend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization := r.Header.Get("Authorization")

		var token auth.TokenResponse

		if !b.AuthTokenAccepted(authorization, &token) {
			log.Printf("Token could not be validated")
			http.Error(w, "Can't validate token", 403)
			return
		}

		if token.Me != b.Me {
			log.Printf("Missing \"me\" in token response: %#v\n", token)
			http.Error(w, "Wrong me", 403)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

func main() {
	log.Println("eksterd - microsub server")
	flag.Parse()

	if authEnabled {
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

	handler := server.NewMicrosubHandler(backend, pool)
	if authEnabled {
		handler = WithAuth(handler, backend.(*memoryBackend))
	}

	http.Handle("/microsub", handler)

	http.Handle("/incoming/", &incomingHandler{
		Backend: &hubBackend,
	})
	handler, err := newMainHandler(backend.(*memoryBackend))
	if err != nil {
		log.Fatal(err)
	}
	http.Handle("/", handler)

	backend.(*memoryBackend).run()
	hubBackend.run()

	log.Printf("Listening on port %d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
