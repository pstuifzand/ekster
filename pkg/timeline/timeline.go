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

// Package timeline contains different types of timeline backends.
//
// "sorted-set" uses Redis sorted sets as a backend
// "stream" uses Redis 5 streams as a backend
// "null" doesn't remember any items added to it
package timeline

import (
	"database/sql"
	"encoding/json"
	"log"

	"p83.nl/go/ekster/pkg/microsub"

	"github.com/gomodule/redigo/redis"
)

// Backend specifies the interface for Timeline. It supports everything that is needed
// for Ekster to implement the channel protocol for Microsub
type Backend interface {
	Items(before, after string) (microsub.Timeline, error)
	Count() (int, error)

	AddItem(item microsub.Item) (bool, error)
	MarkRead(uids []string) error

	// Not used at the moment
	// MarkUnread(uids []string) error
}

// Create creates a channel of the specified type. Return nil when the type
// is not known.
func Create(channel, timelineType string, pool *redis.Pool, db *sql.DB) Backend {
	if timelineType == "sorted-set" {
		timeline := &redisSortedSetTimeline{channel: channel, pool: pool}
		err := timeline.Init()
		if err != nil {
			return nil
		}
		return timeline
	}

	if timelineType == "stream" {
		timeline := &redisStreamTimeline{channel: channel, pool: pool}
		err := timeline.Init()
		if err != nil {
			return nil
		}
		return timeline
	}

	if timelineType == "null" {
		timeline := &nullTimeline{channel: channel}
		err := timeline.Init()
		if err != nil {
			return nil
		}
		return timeline
	}

	if timelineType == "postgres-stream" {
		timeline := &postgresStream{database: db, channel: channel}
		err := timeline.Init()
		if err != nil {
			log.Printf("Error while creating %s: %v", channel, err)
			return nil
		}
		return timeline
	}

	return nil
}

type redisItem struct {
	ID        string
	Published string
	Read      bool
	Data      []byte
}

func (ri *redisItem) Item() microsub.Item {
	var item microsub.Item
	_ = json.Unmarshal(ri.Data, &item)
	return item
}
