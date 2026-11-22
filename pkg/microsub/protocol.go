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

// Package microsub describes the protocol methods of the Microsub protocol
package microsub

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/pstuifzand/ekster/pkg/sse"
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

// Constants for Unread
const (
	UnreadBool  = 0
	UnreadCount = 1
)

// Unread is a special int/bool value for the JSON response
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

// Card contains the fields of an author or location.
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

// Content contains the Text or HTML content of an Item.
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
	Video      []string        `json:"video,omitempty" mf2:"video"`
	Audio      []string        `json:"audio,omitempty" mf2:"audio"`
	LikeOf     []string        `json:"like-of,omitempty" mf2:"like-of"`
	BookmarkOf []string        `json:"bookmark-of,omitempty" mf2:"bookmark-of"`
	RepostOf   []string        `json:"repost-of,omitempty" mf2:"repost-of"`
	InReplyTo  []string        `json:"in-reply-to,omitempty" mf2:"in-reply-to"`
	MentionOf  []string        `json:"mention-of,omitempty" mf2:"mention-of"`
	Content    *Content        `json:"content,omitempty" mf2:"content"`
	Summary    string          `json:"summary,omitempty" mf2:"summary"`
	Latitude   string          `json:"latitude,omitempty" mf2:"latitude"`
	Longitude  string          `json:"longitude,omitempty" mf2:"longitude"`
	Checkin    *Card           `json:"checkin,omitempty" mf2:"checkin"`
	Refs       map[string]Item `json:"refs,omitempty"`
	ID         string          `json:"_id,omitempty"`
	Read       bool            `json:"_is_read"`
	Source     *Source         `json:"_source,omitempty"`
}

// Source is an Item source
type Source struct {
	ID    string `json:"_id"`
	URL   string `json:"url"`
	Name  string `json:"name"`
	Photo string `json:"photo"`
}

// MarshalJSON writes an item, except that a read state is only written for an
// item that has a channel to be read in. A nested ref, a scraped card and a
// fetched ancestor have no _id, so `_is_read: false` on one of them is a fact
// the server invented — and a reader that trusts it is being told something
// about a post the user never subscribed to.
func (i Item) MarshalJSON() ([]byte, error) {
	// The alias has no MarshalJSON of its own, so this cannot recurse.
	type plain Item
	raw, err := json.Marshal(plain(i))
	if err != nil {
		return nil, err
	}
	if i.ID != "" {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "_is_read")
	return json.Marshal(fields)
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

// Constants for Thread depth: the default walk up the in-reply-to chain, and
// the hard cap a server enforces whatever the client asks for. A conversation
// is capped rather than paged, so the cap is the only thing standing between a
// request and an unbounded number of outbound fetches.
const (
	DefaultThreadDepth = 10
	MaxThreadDepth     = 20
)

// ThreadInfo describes the conversation a Thread response covers.
//
// Root is the permalink of the topmost ancestor that could be resolved.
// Partial says the server knows of ancestors it could not return, and Missing
// is how many of them it knows about: the count behind "earlier posts on the
// original site". A thread is never presented as the complete conversation,
// because the server only ever sees the part of it that is indexed.
type ThreadInfo struct {
	Root    string `json:"root,omitempty"`
	Partial bool   `json:"partial"`
	Missing int    `json:"missing"`
}

// Thread is the conversation around one entry.
//
// Items are in conversation order: the root first, then the known replies
// ordered by published ascending. Each item keeps its in-reply-to, so a client
// rebuilds the tree without a second request. There is deliberately no paging
// object: a conversation is not a page, and servers cap a thread instead.
type Thread struct {
	Items []Item `json:"items"`
	// Refs are the posts the anchor refers to without answering them:
	// mention-of, like-of, repost-of, bookmark-of. They are not the
	// conversation, so they are not in Items; they are what the entry is
	// about, and without them a thread on a post that only mentions another
	// post would be a single item and nothing else.
	Refs   map[string]Item `json:"refs,omitempty"`
	Thread ThreadInfo      `json:"thread"`
}

// MarshalJSON writes a thread response. An entry outside the user's channels
// carries no _id, because there is no channel in which it could be marked
// read, so it must not claim a read state either: _is_read on such an entry
// would be a fact the server invented.
func (t Thread) MarshalJSON() ([]byte, error) {
	items := make([]map[string]interface{}, 0, len(t.Items))
	for _, item := range t.Items {
		fields, err := threadFields(item)
		if err != nil {
			return nil, err
		}
		items = append(items, fields)
	}

	var refs map[string]map[string]interface{}
	if len(t.Refs) > 0 {
		refs = make(map[string]map[string]interface{}, len(t.Refs))
		for target, item := range t.Refs {
			fields, err := threadFields(item)
			if err != nil {
				return nil, err
			}
			refs[target] = fields
		}
	}

	return json.Marshal(struct {
		Items  []map[string]interface{}          `json:"items"`
		Refs   map[string]map[string]interface{} `json:"refs,omitempty"`
		Thread ThreadInfo                        `json:"thread"`
	}{items, refs, t.Thread})
}

// threadFields is one item as JSON, without a read state it cannot have.
func threadFields(item Item) (map[string]interface{}, error) {
	raw, err := json.Marshal(item)
	if err != nil {
		return nil, err
	}
	var fields map[string]interface{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	if item.ID == "" {
		delete(fields, "_is_read")
	}
	return fields, nil
}

// Feed is one microsub feed.
type Feed struct {
	Type        string `json:"type"`
	URL         string `json:"url"`
	Name        string `json:"name,omitempty"`
	Photo       string `json:"photo,omitempty"`
	Description string `json:"description,omitempty"`
	Author      Card   `json:"author,omitempty"`
	// HomePage is the publication's own site, which a feed names separately
	// from the URL of the feed document. It is deliberately not serialized: a
	// follow item's url is the feed the user subscribed to (and what an
	// unfollow sends), while an item's _source.url is the page a reader should
	// be handed when they click the publication.
	HomePage string `json:"-"`
}

// ItemSearchOptions describes content search filters. Repeated filter values
// are alternatives; different filters are combined.
type ItemSearchOptions struct {
	Channel, Query                 string
	Author, Category, Kind, Source []string
	Before, After                  string
	Limit                          int
}

// Microsub is the main protocol that should be implemented by a backend
type Microsub interface {
	ChannelsGetList(ctx context.Context) ([]Channel, error)
	ChannelsCreate(ctx context.Context, name string) (Channel, error)
	ChannelsUpdate(ctx context.Context, uid, name string) (Channel, error)
	ChannelsDelete(ctx context.Context, uid string) error
	ChannelsOrder(ctx context.Context, uids []string) error

	TimelineGet(ctx context.Context, before, after, channel string) (Timeline, error)
	TimelineGetWithLimit(ctx context.Context, before, after, channel string, limit int) (Timeline, error)

	// ThreadGet returns the conversation around one entry: the root first,
	// then the known replies. entry is an _id inside the user's channels and
	// url is a permalink; exactly one of them is used. depth bounds the walk
	// up the in-reply-to chain.
	ThreadGet(ctx context.Context, channel, entry, url string, depth int) (Thread, error)

	MarkRead(ctx context.Context, channel string, entry []string) error
	MarkUnread(ctx context.Context, channel string, entry []string) error
	MarkReadThrough(ctx context.Context, channel, lastReadEntry string) error
	TimelineRemove(ctx context.Context, channel string, entry []string) error

	FollowGetList(ctx context.Context, uid string) ([]Feed, error)
	FollowURL(ctx context.Context, uid string, url string) (Feed, error)

	UnfollowURL(ctx context.Context, uid string, url string) error

	Search(ctx context.Context, query string) ([]Feed, error)
	PreviewURL(ctx context.Context, url string) (Timeline, error)

	ItemSearch(ctx context.Context, channel, query string) ([]Item, error)
	ItemSearchWithOptions(ctx context.Context, options ItemSearchOptions) (Timeline, error)

	MuteGetList(ctx context.Context, channel string) ([]string, error)
	MuteURL(ctx context.Context, channel, url string) error
	UnmuteURL(ctx context.Context, channel, url string) error
	BlockGetList(ctx context.Context, channel string) ([]Card, error)
	BlockURL(ctx context.Context, channel, url string) error
	UnblockURL(ctx context.Context, channel, url string) error

	Events(ctx context.Context) (chan sse.Message, error)
}

// MarshalJSON encodes an Unread value as JSON
func (unread Unread) MarshalJSON() ([]byte, error) {
	switch unread.Type {
	case UnreadBool:
		return json.Marshal(unread.Unread)
	case UnreadCount:
		return json.Marshal(unread.UnreadCount)
	}
	return json.Marshal(nil)
}

// UnmarshalJSON decodes an Unread value from JSON
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

// String returns a string of the unread value
func (unread Unread) String() string {
	switch unread.Type {
	case UnreadBool:
		return fmt.Sprint(unread.Unread)
	case UnreadCount:
		return fmt.Sprint(unread.UnreadCount)
	}
	return ""
}

// HasUnread return true of there are unread items.
func (unread *Unread) HasUnread() bool {
	switch unread.Type {
	case UnreadBool:
		return unread.Unread
	case UnreadCount:
		return unread.UnreadCount > 0
	}
	return false
}
