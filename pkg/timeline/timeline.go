// Package timeline contains different types of timeline backends.
//
// "sorted-set" uses Redis sorted sets as a backend
// "stream" uses Redis 5 streams as a backend
// "null" doesn't remember any items added to it
package timeline

import (
	"encoding/json"

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

// Create creates a channel of the specfied type. Return nil when the type
// is not known.
func Create(channel, timelineType string, pool *redis.Pool) Backend {
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
