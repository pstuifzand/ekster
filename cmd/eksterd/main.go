// Copyright (C) 2018 Peter Stuifzand
//
// This program is free software: you can redistribute it and/or modify it under the terms of the GNU General Public
// License as published by the Free Software Foundation, either version 3 of the License, or (at your option) any
// later version.
//
// This program is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the implied
// warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License along with this program. If not,
// see <http://www.gnu.org/licenses/>.

/*
Eksterd is a microsub server that is extendable.
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
	"github.com/pkg/errors"
	"p83.nl/go/ekster/pkg/auth"

	"p83.nl/go/ekster/pkg/server"
)

// AppOptions are options for the app
type AppOptions struct {
	Port        int
	AuthEnabled bool
	Headless    bool
	RedisServer string
	BaseURL     string
	TemplateDir string
	pool        *redis.Pool
}

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
}

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

// WithAuth adds authorization to a http.Handler
func WithAuth(handler http.Handler, b *memoryBackend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			handler.ServeHTTP(w, r)
			return
		}

		authorization := ""

		values := r.URL.Query()

		if r.Method == http.MethodGet && values.Get("action") == "events" && values.Get("access_token") != "" {
			authorization = "Bearer " + values.Get("access_token")
		} else {
			authorization = r.Header.Get("Authorization")
		}

		var token auth.TokenResponse

		authorized, err := b.AuthTokenAccepted(authorization, &token)
		if err != nil {
			log.Printf("token not accepted: %v", err)
		}
		if !authorized {
			log.Printf("Token could not be validated")
			http.Error(w, "Can't validate token", 403)
			return
		}

		if token.Me != b.Me { // FIXME: Me should be part of the request
			log.Printf("Missing \"me\" in token response: %#v\n", token)
			http.Error(w, "Wrong me", 403)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

// App is the main app structure
type App struct {
	options    AppOptions
	backend    *memoryBackend
	hubBackend *hubIncomingBackend
}

// Run runs the app
func (app *App) Run() error {
	app.backend.run()
	app.hubBackend.run()

	log.Printf("Listening on port %d\n", app.options.Port)
	return http.ListenAndServe(fmt.Sprintf(":%d", app.options.Port), nil)
}

// NewApp initializes the App
func NewApp(options AppOptions) (*App, error) {
	app := &App{
		options: options,
	}

	backend, err := loadMemoryBackend(options.pool)
	if err != nil {
		return nil, err
	}
	app.backend = backend
	app.backend.AuthEnabled = options.AuthEnabled
	app.backend.baseURL = options.BaseURL
	app.backend.hubIncomingBackend.pool = options.pool
	app.backend.hubIncomingBackend.baseURL = options.BaseURL

	app.hubBackend = &hubIncomingBackend{backend: app.backend, baseURL: options.BaseURL, pool: options.pool}

	http.Handle("/micropub", &micropubHandler{
		Backend: app.backend,
		pool:    options.pool,
	})

	handler, broker := server.NewMicrosubHandler(app.backend)
	if options.AuthEnabled {
		handler = WithAuth(handler, app.backend)
	}

	app.backend.broker = broker

	http.Handle("/microsub", handler)

	http.Handle("/incoming/", &incomingHandler{
		Backend: app.hubBackend,
	})

	if !options.Headless {
		handler, err := newMainHandler(app.backend, options.BaseURL, options.TemplateDir, options.pool)
		if err != nil {
			return nil, errors.Wrap(err, "could not create main handler")
		}
		http.Handle("/", handler)
	}

	return app, nil
}

func main() {
	log.Println("eksterd - microsub server")

	var options AppOptions

	flag.IntVar(&options.Port, "port", 80, "port for serving api")
	flag.BoolVar(&options.AuthEnabled, "auth", true, "use auth")
	flag.BoolVar(&options.Headless, "headless", false, "disable frontend")
	flag.StringVar(&options.RedisServer, "redis", "redis:6379", "redis server")
	flag.StringVar(&options.BaseURL, "baseurl", "", "http server baseurl")
	flag.StringVar(&options.TemplateDir, "templates", "./templates", "template directory")

	flag.Parse()

	if options.AuthEnabled {
		log.Println("Using auth")
	} else {
		log.Println("Authentication disabled")
	}

	if options.BaseURL == "" {
		if envVar, e := os.LookupEnv("EKSTER_BASEURL"); e {
			options.BaseURL = envVar
		} else {
			log.Fatal("EKSTER_BASEURL environment variable not found, please set with external url, -baseurl url option")
		}
	}

	if options.TemplateDir == "" {
		if envVar, e := os.LookupEnv("EKSTER_TEMPLATES"); e {
			options.TemplateDir = envVar
		} else {
			log.Fatal("EKSTER_TEMPLATES environment variable not found, use env var or -templates dir option")
		}
	}

	createBackend := false
	args := flag.Args()

	if len(args) >= 1 {
		if args[0] == "new" {
			createBackend = true
		}
	}

	if createBackend {
		err := createMemoryBackend()
		if err != nil {
			log.Fatalf("Error while saving backend.json: %s", err)
		}

		// TODO(peter): automatically gather this information from login or otherwise
		log.Println(`Config file "backend.json" is created in the current directory.`)
		log.Println(`Update "Me" variable to your website address "https://example.com/"`)
		log.Println(`Update "TokenEndpoint" variable to the address of your token endpoint "https://example.com/token"`)

		return
	}

	pool := newPool(options.RedisServer)
	options.pool = pool

	app, err := NewApp(options)
	if err != nil {
		log.Fatal(err)
	}

	err = app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
