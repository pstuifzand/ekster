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
func (b *NullBackend) ChannelsGetList() ([]microsub.Channel, error) {
	return []microsub.Channel{
		microsub.Channel{UID: "0000", Name: "default", Unread: 0},
		microsub.Channel{UID: "0001", Name: "notifications", Unread: 0},
		microsub.Channel{UID: "1000", Name: "Friends", Unread: 0},
		microsub.Channel{UID: "1001", Name: "Family", Unread: 0},
	}, nil
}

// ChannelsCreate creates no channels
func (b *NullBackend) ChannelsCreate(name string) (microsub.Channel, error) {
	return microsub.Channel{
		UID:  "1234",
		Name: name,
	}, nil
}

// ChannelsUpdate updates no channels
func (b *NullBackend) ChannelsUpdate(uid, name string) (microsub.Channel, error) {
	return microsub.Channel{
		UID:  uid,
		Name: name,
	}, nil
}

// ChannelsDelete delets no channels
func (b *NullBackend) ChannelsDelete(uid string) error {
	return nil
}

// TimelineGet gets no timeline
func (b *NullBackend) TimelineGet(before, after, channel string) (microsub.Timeline, error) {
	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  []microsub.Item{},
	}, nil
}

func (b *NullBackend) FollowGetList(uid string) ([]microsub.Feed, error) {
	return []microsub.Feed{}, nil
}

func (b *NullBackend) FollowURL(uid string, url string) (microsub.Feed, error) {
	return microsub.Feed{Type: "feed", URL: url}, nil
}

func (b *NullBackend) UnfollowURL(uid string, url string) error {
	return nil
}

func (b *NullBackend) Search(query string) ([]microsub.Feed, error) {
	return []microsub.Feed{}, nil
}

func (b *NullBackend) PreviewURL(url string) (microsub.Timeline, error) {
	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  []microsub.Item{},
	}, nil
}

func (b *NullBackend) MarkRead(channel string, uids []string) error {
	return nil
}
