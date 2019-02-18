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

// Package microsub describes the protocol methods of the Microsub protocol
package microsub

import (
	"encoding/json"
	"fmt"
)

/*
	channels
	search
	preview
	follow / unfollow
	timeline
	mute / unmute
	block / unblock
*/

const (
	UnreadBool  = 0
	UnreadCount = 1
)

type Unread struct {
	Type        int
	Unread      bool
	UnreadCount int
}

// Channel contains information about a channel.
type Channel struct {
	// UID is a unique id for the channel
	UID    string `json:"uid"`
	Name   string `json:"name"`
	Unread Unread `json:"unread,omitempty"`
}

type Card struct {
	// Filled      bool   `json:"filled,omitempty"`
	Type        string `json:"type,omitempty"`
	Name        string `json:"name,omitempty" mf2:"name"`
	URL         string `json:"url,omitempty" mf2:"url"`
	Photo       string `json:"photo,omitempty" mf2:"photo"`
	Locality    string `json:"locality,omitempty" mf2:"locality"`
	Region      string `json:"region,omitempty" mf2:"region"`
	CountryName string `json:"country-name,omitempty" mf2:"country-name"`
	Longitude   string `json:"longitude,omitempty" mf2:"longitude"`
	Latitude    string `json:"latitude,omitempty" mf2:"latitude"`
}

type Content struct {
	Text string `json:"text,omitempty" mf2:"value"`
	HTML string `json:"html,omitempty" mf2:"html"`
}

// Item is a post object
type Item struct {
	Type       string          `json:"type"`
	Name       string          `json:"name,omitempty" mf2:"name"`
	Published  string          `json:"published,omitempty" mf2:"published"`
	Updated    string          `json:"updated,omitempty" mf2:"updated"`
	URL        string          `json:"url,omitempty" mf2:"url"`
	UID        string          `json:"uid,omitempty" mf2:"uid"`
	Author     *Card           `json:"author,omitempty" mf2:"author"`
	Category   []string        `json:"category,omitempty" mf2:"category"`
	Photo      []string        `json:"photo,omitempty" mf2:"photo"`
	LikeOf     []string        `json:"like-of,omitempty" mf2:"like-of"`
	BookmarkOf []string        `json:"bookmark-of,omitempty" mf2:"bookmark-of"`
	RepostOf   []string        `json:"repost-of,omitempty" mf2:"repost-of"`
	InReplyTo  []string        `json:"in-reply-to,omitempty" mf2:"in-reply-to"`
	Content    *Content        `json:"content,omitempty" mf2:"content"`
	Summary    string          `json:"summary,omitempty" mf2:"summary"`
	Latitude   string          `json:"latitude,omitempty" mf2:"latitude"`
	Longitude  string          `json:"longitude,omitempty" mf2:"longitude"`
	Checkin    *Card           `json:"checkin,omitempty" mf2:"checkin"`
	Refs       map[string]Item `json:"refs,omitempty"`
	ID         string          `json:"_id,omitempty"`
	Read       bool            `json:"_is_read"`
}

// Pagination contains information about paging
type Pagination struct {
	After  string `json:"after,omitempty"`
	Before string `json:"before,omitempty"`
}

// Timeline is a combination of items and paging information
type Timeline struct {
	Items  []Item     `json:"items"`
	Paging Pagination `json:"paging"`
}

type Feed struct {
	Type        string `json:"type"`
	URL         string `json:"url"`
	Name        string `json:"name,omitempty"`
	Photo       string `json:"photo,omitempty"`
	Description string `json:"description,omitempty"`
	Author      Card   `json:"author,omitempty"`
}

type Message string

type Event struct {
	Msg Message
}

type EventListener interface {
	WriteMessage(evt Event)
}

// Microsub is the main protocol that should be implemented by a backend
type Microsub interface {
	ChannelsGetList() ([]Channel, error)
	ChannelsCreate(name string) (Channel, error)
	ChannelsUpdate(uid, name string) (Channel, error)
	ChannelsDelete(uid string) error

	TimelineGet(before, after, channel string) (Timeline, error)

	MarkRead(channel string, entry []string) error

	FollowGetList(uid string) ([]Feed, error)
	FollowURL(uid string, url string) (Feed, error)

	UnfollowURL(uid string, url string) error

	Search(query string) ([]Feed, error)
	PreviewURL(url string) (Timeline, error)
}

func (unread Unread) MarshalJSON() ([]byte, error) {
	switch unread.Type {
	case UnreadBool:
		return json.Marshal(unread.Unread)
	case UnreadCount:
		return json.Marshal(unread.UnreadCount)
	}
	return json.Marshal(nil)
}

func (unread *Unread) UnmarshalJSON(bytes []byte) error {
	var b bool
	err := json.Unmarshal(bytes, &b)
	if err == nil {
		unread.Type = UnreadBool
		unread.Unread = b
		return nil
	}

	var count int
	err = json.Unmarshal(bytes, &count)
	if err == nil {
		unread.Type = UnreadCount
		unread.UnreadCount = count
		return nil
	}

	return fmt.Errorf("can't unmarshal as bool or int")
}

func (unread Unread) String() string {
	switch unread.Type {
	case UnreadBool:
		return fmt.Sprint(unread.Unread)
	case UnreadCount:
		return fmt.Sprint(unread.UnreadCount)
	}
	return ""
}

func (unread *Unread) HasUnread() bool {
	switch unread.Type {
	case UnreadBool:
		return unread.Unread
	case UnreadCount:
		return unread.UnreadCount > 0
	}
	return false
}
