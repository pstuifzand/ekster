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
	UID  string `json:"uid"`
	Name string `json:"name"`
}

type Author struct {
	Filled bool   `json:"-"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	URL    string `json:"url"`
	Photo  string `json:"photo"`
}

type Content struct {
	Text string `json:"text"`
	HTML string `json:"html"`
}

// Item is a post object
type Item struct {
	Type       string   `json:"type"`
	Name       string   `json:"name,omitempty"`
	Published  string   `json:"published"`
	URL        string   `json:"url"`
	Author     Author   `json:"author"`
	Category   []string `json:"category"`
	Photo      []string `json:"photo"`
	LikeOf     []string `json:"like-of"`
	BookmarkOf []string `json:"bookmark-of"`
	InReplyTo  []string `json:"in-reply-to"`
	Summary    []string `json:"summary,omitempty"`
	Content    Content  `json:"content,omitempty"`
	Latitude   string   `json:"latitude,omitempty"`
	Longitude  string   `json:"longitude,omitempty"`
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
	Type string `json:"type"`
	URL  string `json:"url"`
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
