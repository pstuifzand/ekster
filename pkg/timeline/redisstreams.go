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

package timeline

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

type redisStreamTimeline struct {
	channel, channelKey string

	pool *redis.Pool
}

/*
 * REDIS STREAMS TIMELINE
 */
func (timeline *redisStreamTimeline) Init() error {
	timeline.channelKey = fmt.Sprintf("stream:%s", timeline.channel)
	return nil
}

func (timeline *redisStreamTimeline) Items(before, after string) (microsub.Timeline, error) {
	conn := timeline.pool.Get()
	defer conn.Close()

	if before == "" {
		before = "-"
	}

	if after == "" {
		after = "+"
	}

	results, err := redis.Values(conn.Do("XREVRANGE", redis.Args{}.Add(timeline.channelKey, after, before, "COUNT", "20")...))
	if err != nil {
		return microsub.Timeline{}, err
	}

	var forRedis redisItem

	var items []microsub.Item
	for _, result := range results {
		if value, ok := result.([]interface{}); ok {
			id, ok2 := value[0].([]uint8)

			if item, ok3 := value[1].([]interface{}); ok3 {
				err = redis.ScanStruct(item, &forRedis)
				if err != nil {
					continue
				}
				item := forRedis.Item()
				if ok2 {
					item.ID = string(id)
				}
				items = append(items, item)
			}
		}
	}

	return microsub.Timeline{
		Items: items,
		Paging: microsub.Pagination{
			After: items[len(items)-1].ID,
		},
	}, nil
}

func (timeline *redisStreamTimeline) AddItem(item microsub.Item) (bool, error) {
	conn := timeline.pool.Get()
	defer conn.Close()

	if item.Published == "" {
		item.Published = time.Now().Format(time.RFC3339)
	}

	data, err := json.Marshal(item)
	if err != nil {
		log.Printf("error while creating item for redis: %v\n", err)
		return false, err
	}

	args := redis.Args{}.Add(timeline.channelKey).Add("*").Add("ID").Add(item.ID).Add("Published").Add(item.Published).Add("Read").Add(item.Read).Add("Data").Add(data)

	_, err = redis.String(conn.Do("XADD", args...))

	_, _ = conn.Do("XTRIM", timeline.channelKey, "MAXLEN", "~", "250")

	return err == nil, err
}

func (timeline *redisStreamTimeline) Count() (int, error) {
	conn := timeline.pool.Get()
	defer conn.Close()

	return redis.Int(conn.Do("XLEN", timeline.channelKey))
}

func (timeline *redisStreamTimeline) MarkRead(uids []string) error {
	// panic("implement me")
	return nil
}

func (timeline *redisStreamTimeline) MarkUnread(uids []string) error {
	// panic("implement me")
	return nil
}
