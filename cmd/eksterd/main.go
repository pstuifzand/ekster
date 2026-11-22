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

/*
Eksterd is a microsub server that is extendable.
*/
package main

import (
	"context"
	"database/sql"
	"embed"
	_ "expvar"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/pstuifzand/ekster/pkg/auth"
	"github.com/pstuifzand/ekster/pkg/observability"
	"github.com/pstuifzand/ekster/pkg/userid"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/gomodule/redigo/redis"
)

// AppOptions are options for the app
type AppOptions struct {
	Port        int
	AuthEnabled bool
	Headless    bool
	RedisServer string
	BaseURL     string
	DatabaseURL string
	pool        *redis.Pool
	database    *sql.DB
}

//go:embed db/migrations/*.sql
var migrations embed.FS

func newPool(addr string) *redis.Pool {
	return &redis.Pool{
		MaxIdle:     3,
		IdleTimeout: 240 * time.Second,
		Dial:        func() (redis.Conn, error) { return redis.Dial("tcp", addr) },
	}
}

// deny answers a rejected request with the CORS headers too. Without them a
// browser sees a network error instead of the reason, which is the one thing a
// reader needs to fix its configuration.
func deny(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, Cache-Control, Last-Event-ID, Content-Type")
	http.Error(w, msg, code)
}

// withoutAuthIdentity selects the identity in the public endpoint path. It
// must only be used when authentication is explicitly disabled.
func withoutAuthIdentity(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			handler.ServeHTTP(w, r)
			return
		}
		id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/microsub/"))
		if !strings.HasPrefix(r.URL.Path, "/microsub/") || err != nil || id <= 0 {
			deny(w, "No user id found in url", http.StatusBadRequest)
			return
		}
		handler.ServeHTTP(w, r.WithContext(userid.NewContext(r.Context(), id)))
	})
}

// WithAuth adds authorization to a http.Handler
func WithAuth(handler http.Handler, b *memoryBackend) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			handler.ServeHTTP(w, r)
			return
		}

		// Get user id from microsub url
		userIDStr := strings.TrimPrefix(r.URL.Path, "/microsub/")
		userID, err := strconv.Atoi(userIDStr)
		if err != nil {
			slog.WarnContext(r.Context(), "path has no user id", "path", r.URL.Path)
			deny(w, "No user id found in url", http.StatusBadRequest)
			return
		}

		var me, tokenEndpoint string
		row := b.database.QueryRow(`SELECT "url", "token_endpoint" FROM "users" WHERE "id" = $1`, userID)
		err = row.Scan(&me, &tokenEndpoint)
		if err == sql.ErrNoRows {
			slog.WarnContext(r.Context(), "no user found", "user_id", userID)
			deny(w, "No user found with id", http.StatusBadRequest)
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

		authorized, err := b.AuthTokenAccepted(authorization, &token, tokenEndpoint)
		if err != nil {
			logAuthFailure(r.Context(), err)
		}
		if !authorized {
			slog.WarnContext(r.Context(), "token could not be validated")
			deny(w, "Can't validate token", http.StatusForbidden)
			return
		}

		if token.Me != me {
			slog.ErrorContext(r.Context(), "token response has no me")
			http.Error(w, "Wrong me", http.StatusForbidden)
			return
		}

		ctx := userid.NewContext(r.Context(), userID)
		r = r.WithContext(ctx)

		handler.ServeHTTP(w, r)
	})
}

func main() {
	setupLogging()
	slog.Info("eksterd starting", "version", BuildVersion())

	var options AppOptions

	flag.IntVar(&options.Port, "port", 80, "port for serving api")
	flag.BoolVar(&options.AuthEnabled, "auth", true, "use auth (if disabled, any caller can select any user via /microsub/{id})")
	flag.BoolVar(&options.Headless, "headless", false, "disable frontend")
	flag.StringVar(&options.RedisServer, "redis", "redis:6379", "redis server")
	flag.StringVar(&options.BaseURL, "baseurl", "", "http server baseurl")
	flag.StringVar(&options.DatabaseURL, "db", "host=database user=postgres password=simple dbname=ekster sslmode=disable", "database url")

	flag.Parse()

	if options.AuthEnabled {
		slog.Info("authentication enabled")
	} else {
		slog.Info("authentication disabled")
	}

	if options.BaseURL == "" {
		if envVar, e := os.LookupEnv("EKSTER_BASEURL"); e {
			options.BaseURL = envVar
		} else {
			slog.Error("EKSTER_BASEURL is not set", "flag", "-baseurl")
			os.Exit(1)
		}
	}

	pool := newPool(options.RedisServer)
	options.pool = pool
	db, err := sql.Open("postgres", options.DatabaseURL)
	if err != nil {
		slog.Error("could not open the database", "error", err)
		os.Exit(1)
	}
	options.database = db

	err = runMigrations(db)
	if err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	db, err = sql.Open("postgres", options.DatabaseURL)
	if err != nil {
		slog.Error("could not open the database", "error", err)
		os.Exit(1)
	}
	options.database = db
	app, err := NewApp(options)
	if err != nil {
		slog.Error("could not create the app", "error", err)
		os.Exit(1)
	}

	// Tracing is opt-in. With OTEL_EXPORTER_OTLP_ENDPOINT set, spans are sent
	// to that collector; nothing is sent otherwise. A broken tracing setup must
	// not keep the server down, so it degrades to a log line.
	shutdown, err := observability.Setup(context.Background(), "ekster", BuildVersion())
	if err != nil {
		slog.Error("could not start tracing", "error", err)
		shutdown = func(context.Context) error { return nil }
	}

	// Metrics are opt-in and live on their own listener: the reverse proxy in
	// front of eksterd forwards every path, so a /metrics route on the public
	// port would be readable by anyone who can reach the Microsub endpoint.
	metricsShutdown, msErr := observability.StartMetricsServer(context.Background(), os.Getenv("EKSTER_METRICS_ADDR"))
	if msErr != nil {
		slog.Error("could not start the metrics server", "error", msErr)
		metricsShutdown = func(context.Context) error { return nil }
	}

	// Per-feed schedule gauges: tier and next fetch are state the ticker reads
	// from the database, so they are exported from the same table rather than
	// from whatever the last fetch span happened to say. Registered whether or
	// not metrics are enabled — the registry is only served when they are.
	if err := observability.Register(newFeedGauges(db)); err != nil {
		slog.Error("could not register the feed gauges", "error", err)
	}

	// The batch exporter holds spans until they are flushed, and a container
	// gets SIGTERM, not a clean return. Flush before leaving so the work that
	// just happened is not lost with the process.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = metricsShutdown(ctx)
		_ = shutdown(ctx)
		os.Exit(0)
	}()

	err = app.Run()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = metricsShutdown(ctx)
	_ = shutdown(ctx)
	cancel()

	db.Close()
	if err != nil {
		slog.Error("eksterd stopped", "error", err)
		os.Exit(1)
	}
}

// Log migrations
type Log struct {
}

// Printf for migrations logs
func (l Log) Printf(format string, v ...interface{}) {
	slog.Info(strings.TrimRight(fmt.Sprintf(format, v...), "\n"))
}

// Verbose returns false
func (l Log) Verbose() bool {
	return false
}

func runMigrations(db *sql.DB) error {
	d, err := iofs.New(migrations, "db/migrations")
	if err != nil {
		return err
	}
	databaseInstance, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", d, "database", databaseInstance)
	if err != nil {
		return err
	}
	defer m.Close()
	m.Log = &Log{}
	slog.Info("running migrations")
	if err = m.Up(); err != nil {
		if err != migrate.ErrNoChange {
			return err
		}
	}
	slog.Info("migrations are up")
	return nil
}
