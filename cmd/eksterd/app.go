package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/pkg/errors"
	"p83.nl/go/ekster/pkg/server"
)

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
