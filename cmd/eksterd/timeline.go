package main

import (
	"p83.nl/go/ekster/pkg/timeline"
)

func (b *memoryBackend) getTimeline(channel string) timeline.Backend {
	timelineType := "sorted-set"
	if channel == "notifications" {
		timelineType = "stream"
	} else {
		if setting, ok := b.Settings[channel]; ok {
			if setting.ChannelType != "" {
				timelineType = setting.ChannelType
			}
		}
	}

	return timeline.Create(channel, timelineType, b.pool)
}
