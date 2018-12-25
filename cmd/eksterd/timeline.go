package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/microsub"
)

type TimelineBackend interface {
	GetItems(before, after string) (microsub.Timeline, error)
	AddItem(item microsub.Item) error

	MarkRead(uid string) error
	MarkUnread(uid string) error
}

type redisSortedSetTimeline struct {
	channel string
}

type redisStreamTimeline struct {
	channel string
}

func GetTimeline(timelineType, channel string) TimelineBackend {
	if timelineType == "sorted-set" {
		return &redisSortedSetTimeline{channel}
	}
	if timelineType == "stream" {
		return &redisStreamTimeline{channel}
	}
	return nil
}

/*
 * REDIS SORTED SETS TIMELINE
 */
func (timeline *redisSortedSetTimeline) GetItems(before, after string) (microsub.Timeline, error) {
	conn := pool.Get()
	defer conn.Close()

	items := []microsub.Item{}

	channel := timeline.channel

	zchannelKey := fmt.Sprintf("zchannel:%s:posts", channel)

	afterScore := "-inf"
	if len(after) != 0 {
		afterScore = "(" + after
	}
	beforeScore := "+inf"
	if len(before) != 0 {
		beforeScore = "(" + before
	}

	var itemJSONs [][]byte

	itemScores, err := redis.Strings(
		conn.Do(
			"ZRANGEBYSCORE",
			zchannelKey,
			afterScore,
			beforeScore,
			"LIMIT",
			0,
			20,
			"WITHSCORES",
		),
	)

	if err != nil {
		return microsub.Timeline{
			Paging: microsub.Pagination{},
			Items:  items,
		}, err
	}

	if len(itemScores) >= 2 {
		before = itemScores[1]
		after = itemScores[len(itemScores)-1]
	} else {
		before = ""
		after = ""
	}

	for i := 0; i < len(itemScores); i += 2 {
		itemID := itemScores[i]
		itemJSON, err := redis.Bytes(conn.Do("HGET", itemID, "Data"))
		if err != nil {
			log.Println(err)
			continue
		}
		itemJSONs = append(itemJSONs, itemJSON)
	}

	for _, obj := range itemJSONs {
		item := microsub.Item{}
		err := json.Unmarshal(obj, &item)
		if err != nil {
			// FIXME: what should we do if one of the items doen't unmarshal?
			log.Println(err)
			continue
		}
		item.Read = false
		items = append(items, item)
	}
	paging := microsub.Pagination{
		After:  after,
		Before: before,
	}

	return microsub.Timeline{
		Paging: paging,
		Items:  items,
	}, nil
}

func (*redisSortedSetTimeline) AddItem(item microsub.Item) error {
	panic("implement me")
}

func (*redisSortedSetTimeline) MarkRead(uid string) error {
	panic("implement me")
}

func (*redisSortedSetTimeline) MarkUnread(uid string) error {
	panic("implement me")
}

/*
 * REDIS STREAMS TIMELINE
 */
func (*redisStreamTimeline) GetItems(before, after string) (microsub.Timeline, error) {
	panic("implement me")
}

func (*redisStreamTimeline) AddItem(item microsub.Item) error {
	panic("implement me")
}

func (*redisStreamTimeline) MarkRead(uid string) error {
	panic("implement me")
}

func (*redisStreamTimeline) MarkUnread(uid string) error {
	panic("implement me")
}
