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

/*
	channels
	search
	preview
	follow / unfollow
	timeline
	mute / unmute
	block / unblock
*/

// Channel contains information about a channel.
type Channel struct {
	// UID is a unique id for the channel
	UID    string `json:"uid"`
	Name   string `json:"name"`
	Unread int    `json:"unread"`
}

type Card struct {
	Filled      bool   `json:"-,omitempty"`
	Type        string `json:"type,omitempty"`
	Name        string `json:"name,omitempty"`
	URL         string `json:"url,omitempty"`
	Photo       string `json:"photo,omitempty"`
	Locality    string `json:"locality,omitempty"`
	Region      string `json:"region,omitempty"`
	CountryName string `json:"country-name,omitempty"`
	Longitude   string `json:"longitude,omitempty"`
	Latitude    string `json:"latitude,omitempty"`
}

type Content struct {
	Text string `json:"text,omitempty"`
	HTML string `json:"html,omitempty"`
}

// Item is a post object
type Item struct {
	Type       string   `json:"type"`
	Name       string   `json:"name,omitempty"`
	Published  string   `json:"published,omitempty"`
	Updated    string   `json:"updated,omitempty"`
	URL        string   `json:"url,omitempty"`
	UID        string   `json:"uid,omitempty"`
	Author     *Card    `json:"author,omitempty"`
	Category   []string `json:"category,omitempty"`
	Photo      []string `json:"photo,omitempty"`
	LikeOf     []string `json:"like-of,omitempty"`
	BookmarkOf []string `json:"bookmark-of,omitempty"`
	RepostOf   []string `json:"repost-of,omitempty"`
	InReplyTo  []string `json:"in-reply-to,omitempty"`
	Summary    []string `json:"summary,omitempty"`
	Content    *Content `json:"content,omitempty"`
	Latitude   string   `json:"latitude,omitempty"`
	Longitude  string   `json:"longitude,omitempty"`
	Checkin    *Card    `json:"checkin,omitempty"`
	ID         string   `json:"_id,omitempty"`
	Read       bool     `json:"_is_read"`
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

// Microsub is the main protocol that should be implemented by a backend
type Microsub interface {
	ChannelsGetList() []Channel
	ChannelsCreate(name string) Channel
	ChannelsUpdate(uid, name string) Channel
	ChannelsDelete(uid string)

	TimelineGet(before, after, channel string) Timeline

	MarkRead(channel string, entry []string)

	FollowGetList(uid string) []Feed
	FollowURL(uid string, url string) Feed

	UnfollowURL(uid string, url string)

	Search(query string) []Feed
	PreviewURL(url string) Timeline
}
