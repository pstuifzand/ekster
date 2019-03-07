package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/microsub"
)

type redisStreamTimeline struct {
	channel, channelKey string
}

/*
 * REDIS STREAMS TIMELINE
 */
func (timeline *redisStreamTimeline) Init() error {
	timeline.channelKey = fmt.Sprintf("stream:%s", timeline.channel)
	return nil
}

func (timeline *redisStreamTimeline) Items(before, after string) (microsub.Timeline, error) {
	conn := pool.Get()
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

func (timeline *redisStreamTimeline) AddItem(item microsub.Item) error {
	conn := pool.Get()
	defer conn.Close()

	if item.Published == "" {
		item.Published = time.Now().Format(time.RFC3339)
	}

	data, err := json.Marshal(item)
	if err != nil {
		log.Printf("error while creating item for redis: %v\n", err)
		return err
	}

	args := redis.Args{}.Add(timeline.channelKey).Add("*").Add("ID").Add(item.ID).Add("Published").Add(item.Published).Add("Read").Add(item.Read).Add("Data").Add(data)

	_, err = redis.String(conn.Do("XADD", args...))

	return err
}

func (timeline *redisStreamTimeline) Count() (int, error) {
	conn := pool.Get()
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
