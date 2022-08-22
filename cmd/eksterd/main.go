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
	"database/sql"
	"embed"
	_ "expvar"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/pstuifzand/ekster/pkg/auth"

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
	TemplateDir string
	DatabaseURL string
	pool        *redis.Pool
	database    *sql.DB
}

//go:embed db/migrations/*.sql
var migrations embed.FS

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
			http.Error(w, "Can't validate token", http.StatusForbidden)
			return
		}

		if token.Me != b.Me { // FIXME: Me should be part of the request
			log.Printf("Missing \"me\" in token response: %#v\n", token)
			http.Error(w, "Wrong me", http.StatusForbidden)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

func main() {
	log.Println("eksterd - microsub server", BuildVersion())

	var options AppOptions

	flag.IntVar(&options.Port, "port", 80, "port for serving api")
	flag.BoolVar(&options.AuthEnabled, "auth", true, "use auth")
	flag.BoolVar(&options.Headless, "headless", false, "disable frontend")
	flag.StringVar(&options.RedisServer, "redis", "redis:6379", "redis server")
	flag.StringVar(&options.BaseURL, "baseurl", "", "http server baseurl")
	flag.StringVar(&options.TemplateDir, "templates", "./templates", "template directory")
	flag.StringVar(&options.DatabaseURL, "db", "host=database user=postgres password=simple dbname=ekster sslmode=disable", "database url")

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
	//
	// createBackend := false
	// args := flag.Args()
	//
	// if len(args) >= 1 {
	// 	if args[0] == "new" {
	// 		createBackend = true
	// 	}
	// }
	//
	// if createBackend {
	// 	err := createMemoryBackend()
	// 	if err != nil {
	// 		log.Fatalf("Error while saving backend.json: %s", err)
	// 	}
	//
	// TODO(peter): automatically gather this information from login or otherwise
	//
	// 	log.Println(`Config file "backend.json" is created in the current directory.`)
	// 	log.Println(`Update "Me" variable to your website address "https://example.com/"`)
	// 	log.Println(`Update "TokenEndpoint" variable to the address of your token endpoint "https://example.com/token"`)
	//
	// 	return
	// }

	pool := newPool(options.RedisServer)
	options.pool = pool
	db, err := sql.Open("postgres", options.DatabaseURL)
	if err != nil {
		log.Fatalf("database open failed: %s", err)
	}
	options.database = db

	err = runMigrations(db)
	if err != nil {
		log.Fatalf("Error with migrations: %s", err)
	}
	app, err := NewApp(options)
	if err != nil {
		log.Fatal(err)
	}

	log.Fatal(app.Run())

	db.Close()
}

// Log migrations
type Log struct {
}

// Printf for migrations logs
func (l Log) Printf(format string, v ...interface{}) {
	log.Printf(format, v...)
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
	log.Println("Running migrations")
	if err = m.Up(); err != nil {
		if err != migrate.ErrNoChange {
			return err
		}
	}
	log.Println("Migrations are up")
	return nil
}
