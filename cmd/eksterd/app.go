/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/pkg/errors"
	"github.com/pstuifzand/ekster/pkg/observability"
	"github.com/pstuifzand/ekster/pkg/server"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// App is the main app structure
type App struct {
	options    AppOptions
	backend    *memoryBackend
	hubBackend *hubIncomingBackend
}

// Run runs the app
func (app *App) Run() error {
	err := initSearch()
	if err != nil {
		return fmt.Errorf("while starting app: %v", err)
	}
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

	backend, err := loadMemoryBackend(options.pool, options.database)
	if err != nil {
		return nil, err
	}
	app.backend = backend

	app.backend.AuthEnabled = options.AuthEnabled

	app.hubBackend = &hubIncomingBackend{
		baseURL:  options.BaseURL,
		pool:     options.pool,
		database: options.database,
	}
	app.backend.hubBackend = app.hubBackend

	http.Handle("/micropub", observability.Handler("/micropub", &micropubHandler{
		Backend: app.backend,
		BaseURL: options.BaseURL,
		pool:    options.pool,
	}, nil))

	http.Handle("/i/", observability.Handler("/i/", &itemHandler{
		Backend: app.backend,
		pool:    options.pool,
	}, nil))

	handler, broker := server.NewMicrosubHandler(app.backend)
	if options.AuthEnabled {
		handler = WithAuth(handler, app.backend)
	}

	app.backend.broker = broker

	// An events request is a long-lived SSE connection. Left instrumented it
	// would be one span per open reader that only ends when the browser does,
	// which says nothing and fills the trace list.
	http.Handle("/microsub/", observability.Handler("/microsub/", handler, func(r *http.Request) bool {
		return r.URL.Query().Get("action") == "events"
	}))

	http.Handle("/incoming/", observability.Handler("/incoming/", &incomingHandler{
		Backend:   app.hubBackend,
		Processor: app.backend,
	}, nil))

	if !options.Headless {
		handler, err := newMainHandler(app.backend, options.BaseURL, options.pool)
		if err != nil {
			return nil, errors.Wrap(err, "could not create main handler")
		}
		http.Handle("/", observability.Handler("/", handler, nil))
	}

	return app, nil
}
