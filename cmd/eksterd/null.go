/*
   Microsub server
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"github.com/pstuifzand/ekster/pkg/microsub"
)

// NullBackend is the simplest possible backend
type NullBackend struct {
}

// ChannelsGetList gets no channels
func (b *NullBackend) ChannelsGetList() []microsub.Channel {
	return []microsub.Channel{
		microsub.Channel{UID: "0000", Name: "default", Unread: 0},
		microsub.Channel{UID: "0001", Name: "notifications", Unread: 0},
		microsub.Channel{UID: "1000", Name: "Friends", Unread: 0},
		microsub.Channel{UID: "1001", Name: "Family", Unread: 0},
	}
}

// ChannelsCreate creates no channels
func (b *NullBackend) ChannelsCreate(name string) microsub.Channel {
	return microsub.Channel{
		UID:  "1234",
		Name: name,
	}
}

// ChannelsUpdate updates no channels
func (b *NullBackend) ChannelsUpdate(uid, name string) microsub.Channel {
	return microsub.Channel{
		UID:  uid,
		Name: name,
	}
}

// ChannelsDelete delets no channels
func (b *NullBackend) ChannelsDelete(uid string) {
}

// TimelineGet gets no timeline
func (b *NullBackend) TimelineGet(before, after, channel string) microsub.Timeline {
	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  []microsub.Item{},
	}
}

func (b *NullBackend) FollowGetList(uid string) []microsub.Feed {
	return []microsub.Feed{}
}

func (b *NullBackend) FollowURL(uid string, url string) microsub.Feed {
	return microsub.Feed{Type: "feed", URL: url}
}

func (b *NullBackend) UnfollowURL(uid string, url string) {
}

func (b *NullBackend) Search(query string) []microsub.Feed {
	return []microsub.Feed{}
}

func (b *NullBackend) PreviewURL(url string) microsub.Timeline {
	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  []microsub.Item{},
	}
}

func (b *NullBackend) MarkRead(channel string, uids []string) {
}
