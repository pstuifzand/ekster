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
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/indieauth"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/util"
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

type session struct {
	AuthorizationEndpoint string `redis:"authorization_endpoint"`
	Me                    string `redis:"me"`
	RedirectURI           string `redis:"redirect_uri"`
	State                 string `redis:"state"`
	ClientID              string `redis:"client_id"`
	LoggedIn              bool   `redis:"logged_in"`
}

type authResponse struct {
	Me string `json:"me"`
}

type indexPage struct {
	Session session
}

func (h *mainHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn := pool.Get()
	defer conn.Close()

	err := r.ParseForm()
	if err != nil {
		log.Println(err)
		http.Error(w, fmt.Sprintf("Bad Request: %s", err.Error()), 400)
		return
	}

	if r.Method == http.MethodGet {
		if r.URL.Path == "/" {
			c, err := r.Cookie("session")
			sessionVar := util.RandStringBytes(16)

			if err == http.ErrNoCookie {
				newCookie := &http.Cookie{
					Name:    "session",
					Value:   sessionVar,
					Expires: time.Now().Add(24 * time.Hour),
				}

				http.SetCookie(w, newCookie)
			} else {
				sessionVar = c.Value
			}

			var sess session
			sessionKey := "session:" + sessionVar
			data, err := redis.Values(conn.Do("HGETALL", sessionKey))
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			err = redis.ScanStruct(data, &sess)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			t, err := template.ParseFiles(
				os.Getenv("EKSTER_TEMPLATES")+"/templates/index.html",
				os.Getenv("EKSTER_TEMPLATES")+"/templates/settings.html",
			)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			var page indexPage
			page.Session = sess

			err = t.ExecuteTemplate(w, "index", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			return
		} else if r.URL.Path == "/auth/callback" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}
			sessionVar := c.Value
			var sess session
			sessionKey := "session:" + sessionVar
			data, err := redis.Values(conn.Do("HGETALL", sessionKey))
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			err = redis.ScanStruct(data, &sess)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			state := r.Form.Get("state")
			if state != sess.State {
				fmt.Fprintf(w, "ERROR: Mismatched state\n")
				return
			}
			code := r.Form.Get("code")

			reqData := url.Values{}
			reqData.Set("code", code)
			reqData.Set("client_id", sess.ClientID)
			reqData.Set("redirect_uri", sess.RedirectURI)

			// resp, err := http.PostForm(sess.AuthorizationEndpoint, reqData)
			req, err := http.NewRequest(http.MethodPost, sess.AuthorizationEndpoint, strings.NewReader(reqData.Encode()))
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			req.Header.Add("Accept", "application/json")
			req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

			log.Println(req)

			client := http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				input := io.TeeReader(resp.Body, os.Stderr)
				dec := json.NewDecoder(input)
				var authResponse authResponse
				err = dec.Decode(&authResponse)
				if err != nil {
					fmt.Fprintf(w, "ERROR: %q\n", err)
					return
				}
				log.Println(authResponse)

				sess.Me = authResponse.Me
				sess.LoggedIn = true
				conn.Do("HMSET", redis.Args{}.Add(sessionKey).AddFlat(sess)...)
				http.Redirect(w, r, "/", 302)
				return
			} else {
				fmt.Fprintf(w, "ERROR: HTTP response code from authorization_endpoint (%s) %d \n", sess.AuthorizationEndpoint, resp.StatusCode)
				return
			}
		}
	} else if r.Method == http.MethodPost {
		if r.URL.Path == "/auth" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}

			sessionVar := c.Value

			// redirect to endpoint
			me := r.Form.Get("url")
			log.Println(me)
			meURL, err := url.Parse(me)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s, %s", err.Error(), me), 400)
				return
			}
			endpoints, err := indieauth.GetEndpoints(meURL)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s %s", err.Error(), me), 400)
				return
			}
			log.Println(endpoints)

			authURL, err := url.Parse(endpoints.AuthorizationEndpoint)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s %s", err.Error(), me), 400)
				return
			}
			log.Println(authURL)

			state := util.RandStringBytes(16)
			clientID := "https://p83.nl/microsub-client"
			redirectURI := fmt.Sprintf("%s/auth/callback", os.Getenv("EKSTER_BASEURL"))

			sess := session{
				AuthorizationEndpoint: endpoints.AuthorizationEndpoint,
				Me:          meURL.String(),
				State:       state,
				RedirectURI: redirectURI,
				ClientID:    clientID,
				LoggedIn:    false,
			}
			conn.Do("HMSET", redis.Args{}.Add("session:"+sessionVar).AddFlat(&sess)...)

			q := authURL.Query()
			q.Add("response_type", "id")
			q.Add("me", meURL.String())
			q.Add("client_id", clientID)
			q.Add("redirect_uri", redirectURI)
			q.Add("state", state)
			authURL.RawQuery = q.Encode()

			http.Redirect(w, r, authURL.String(), 302)
			return
		} else if r.URL.Path == "/auth/logout" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}
			sessionVar := c.Value
			conn.Do("DEL", "session:"+sessionVar)
			http.Redirect(w, r, "/", 302)
			return
		}
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
	hubBackend.run()

	log.Printf("Listening on port %d\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", port), nil))
}
