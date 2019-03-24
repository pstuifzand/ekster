package server

import (
	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/sse"
)

// NullBackend is the simplest possible backend
type NullBackend struct {
}

// ChannelsGetList gets no channels
func (b *NullBackend) ChannelsGetList() ([]microsub.Channel, error) {
	return []microsub.Channel{
		{UID: "0001", Name: "notifications", Unread: microsub.Unread{Type: microsub.UnreadBool, Unread: false}},
		{UID: "0000", Name: "default", Unread: microsub.Unread{Type: microsub.UnreadCount, UnreadCount: 0}},
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

// FollowGetList implements the follow list command
func (b *NullBackend) FollowGetList(uid string) ([]microsub.Feed, error) {
	return []microsub.Feed{
		{Name: "test", Type: "feed", URL: "https://example.com/"},
	}, nil
}

// FollowURL follows a new url
func (b *NullBackend) FollowURL(uid string, url string) (microsub.Feed, error) {
	return microsub.Feed{Type: "feed", URL: url}, nil
}

// UnfollowURL unfollows a url
func (b *NullBackend) UnfollowURL(uid string, url string) error {
	return nil
}

// Search search for a query and return an example list of feeds
func (b *NullBackend) Search(query string) ([]microsub.Feed, error) {
	return []microsub.Feed{
		{Type: "feed", URL: "https://example.com/", Name: "Example", Photo: "test.jpg", Description: "test"},
	}, nil
}

// PreviewURL shows an empty feed
func (b *NullBackend) PreviewURL(url string) (microsub.Timeline, error) {
	return microsub.Timeline{
		Paging: microsub.Pagination{},
		Items:  []microsub.Item{},
	}, nil
}

// MarkRead marks no items as read
func (b *NullBackend) MarkRead(channel string, uids []string) error {
	return nil
}

// Events returns a closed channel.
func (b *NullBackend) Events() (chan sse.Message, error) {
	ch := make(chan sse.Message)
	close(ch)
	return ch, nil
}
