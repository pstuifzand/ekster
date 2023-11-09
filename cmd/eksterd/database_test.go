/*
 *  Ekster is a microsub server
 *  Copyright (c) 2022 The Ekster authors
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
	"database/sql"
	"log"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gomodule/redigo/redis"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type DatabaseSuite struct {
	suite.Suite

	URL      string
	Database *sql.DB

	RedisURL string
	Redis    redis.Conn
}

func (s *DatabaseSuite) SetupSuite() {
	db, err := sql.Open("postgres", s.URL)
	if err != nil {
		log.Fatal(err)
	}
	s.Database = db

	conn, err := redis.Dial("tcp", s.RedisURL)
	if err != nil {
		log.Fatal(err)
	}
	s.Redis = conn
	_, err = s.Redis.Do("SELECT", "1")
	if err != nil {
		log.Fatal(err)
	}
}

func (s *DatabaseSuite) TearDownSuite() {
	err := s.Database.Close()
	if err != nil {
		log.Fatal(err)
	}

	err = s.Redis.Close()
	if err != nil {
		log.Fatal(err)
	}
}

type databaseSuite struct {
	DatabaseSuite
}

func (d *databaseSuite) TestGetChannelFromAuthorization() {
	_, err := d.Database.Exec(`truncate "sources", "channels", "feeds", "subscriptions","items"`)
	assert.NoError(d.T(), err, "truncate sources, channels, feeds")
	row := d.Database.QueryRow(`INSERT INTO "channels" (uid, name, created_at, updated_at) VALUES ('abcdef', 'Channel', now(), now()) RETURNING "id"`)
	var id int
	err = row.Scan(&id)
	assert.NoError(d.T(), err, "insert channel")
	_, err = d.Database.Exec(`INSERT INTO "sources" (channel_id, auth_code, created_at, updated_at) VALUES ($1, '1234', now(), now())`, id)
	assert.NoError(d.T(), err, "insert sources")

	// source_id found
	r := httptest.NewRequest("POST", "/micropub?source_id=1234", nil)
	_, c, err := getChannelFromAuthorization(r, d.Redis, d.Database)
	assert.NoError(d.T(), err, "channel from source_id")
	assert.Equal(d.T(), "abcdef", c, "channel uid found")

	// source_id not found
	r = httptest.NewRequest("POST", "/micropub?source_id=1111", nil)
	_, c, err = getChannelFromAuthorization(r, d.Redis, d.Database)
	assert.Error(d.T(), err, "channel from authorization header")
	assert.Equal(d.T(), "", c, "channel uid found")
}

func TestDatabaseSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skip test for database")
	}

	databaseURL := os.Getenv("DATABASE_TEST_URL")
	if databaseURL == "" {
		databaseURL = "host=database user=postgres password=simple dbname=ekster_testing sslmode=disable"
	}
	databaseSuite := &databaseSuite{
		DatabaseSuite{
			URL:      databaseURL,
			RedisURL: "redis:6379",
		},
	}

	databaseURL = "postgres://postgres@database/ekster_testing?sslmode=disable&user=postgres&password=simple"
	err := runMigrations(databaseURL)
	if err != nil {
		log.Fatal(err)
	}

	suite.Run(t, databaseSuite)
}
