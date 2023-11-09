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

package main

// func Test_memoryBackend_ChannelsCreate(t *testing.T) {
// 	type fields struct {
// 		hubIncomingBackend hubIncomingBackend
// 		lock               sync.RWMutex
// 		Channels           map[string]microsub.Channel
// 		Feeds              map[string][]microsub.Feed
// 		Settings           map[string]channelSetting
// 		NextUID            int
// 		Me                 string
// 		TokenEndpoint      string
// 		AuthEnabled        bool
// 		ticker             *time.Ticker
// 		quit               chan struct{}
// 		broker             *sse.Broker
// 		pool               *redis.Pool
// 	}
// 	type args struct {
// 		name string
// 	}
// 	tests := []struct {
// 		name    string
// 		fields  fields
// 		args    args
// 		want    microsub.Channel
// 		wantErr bool
// 	}{
// 		{
// 			name: "Duplicate channel",
// 			fields: fields{
// 				hubIncomingBackend: hubIncomingBackend{},
// 				lock:               sync.RWMutex{},
// 				Channels: func() map[string]microsub.Channel {
// 					channels := make(map[string]microsub.Channel)
// 					channels["1234"] = microsub.Channel{
// 						UID:  "1234",
// 						Name: "Test",
// 						Unread: microsub.Unread{
// 							Type:        microsub.UnreadCount,
// 							Unread:      false,
// 							UnreadCount: 0,
// 						},
// 					}
// 					return channels
// 				}(),
// 				Feeds: func() map[string][]microsub.Feed {
// 					feeds := make(map[string][]microsub.Feed)
// 					return feeds
// 				}(),
// 				Settings:      nil,
// 				NextUID:       1,
// 				Me:            "",
// 				TokenEndpoint: "",
// 				AuthEnabled:   false,
// 				ticker:        nil,
// 				quit:          nil,
// 				broker:        nil,
// 				pool:          nil,
// 			},
// 			args: args{
// 				name: "Test",
// 			},
// 			want: microsub.Channel{
// 				UID:  "1234",
// 				Name: "Test",
// 				Unread: microsub.Unread{
// 					Type:        microsub.UnreadCount,
// 					Unread:      false,
// 					UnreadCount: 0,
// 				},
// 			},
// 			wantErr: false,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			b := &memoryBackend{
// 				hubIncomingBackend: tt.fields.hubIncomingBackend,
// 				lock:               tt.fields.lock,
// 				Channels:           tt.fields.Channels,
// 				Feeds:              tt.fields.Feeds,
// 				Settings:           tt.fields.Settings,
// 				NextUID:            tt.fields.NextUID,
// 				Me:                 tt.fields.Me,
// 				TokenEndpoint:      tt.fields.TokenEndpoint,
// 				AuthEnabled:        tt.fields.AuthEnabled,
// 				ticker:             tt.fields.ticker,
// 				quit:               tt.fields.quit,
// 				broker:             tt.fields.broker,
// 				pool:               tt.fields.pool,
// 			}
// 			got, err := b.ChannelsCreate(tt.args.name)
// 			if (err != nil) != tt.wantErr {
// 				t.Errorf("ChannelsCreate() error = %v, wantErr %v", err, tt.wantErr)
// 				return
// 			}
// 			if !reflect.DeepEqual(got, tt.want) {
// 				t.Errorf("ChannelsCreate() got = %v, want %v", got, tt.want)
// 			}
// 		})
// 	}
// }
//
// func Test_memoryBackend_removeFeed(t *testing.T) {
// 	type fields struct {
// 		Channels map[string]microsub.Channel
// 		Feeds    map[string][]microsub.Feed
// 	}
// 	type args struct {
// 		feedID string
// 	}
// 	tests := []struct {
// 		name    string
// 		fields  fields
// 		args    args
// 		lens    map[string]int
// 		wantErr bool
// 	}{
// 		{
// 			name: "remove from channel 1",
// 			fields: fields{
// 				Channels: map[string]microsub.Channel{
// 					"123": {UID: "channel1", Name: "Channel 1"},
// 					"124": {UID: "channel2", Name: "Channel 2"},
// 				},
// 				Feeds: map[string][]microsub.Feed{
// 					"123": {{Type: "feed", URL: "feed1", Name: "Feed1"}},
// 					"124": {{Type: "feed", URL: "feed2", Name: "Feed2"}},
// 				},
// 			},
// 			args:    args{feedID: "feed1"},
// 			lens:    map[string]int{"123": 0, "124": 1},
// 			wantErr: false,
// 		},
// 		{
// 			name: "remove from channel 2",
// 			fields: fields{
// 				Channels: map[string]microsub.Channel{
// 					"123": {UID: "channel1", Name: "Channel 1"},
// 					"124": {UID: "channel2", Name: "Channel 2"},
// 				},
// 				Feeds: map[string][]microsub.Feed{
// 					"123": {{Type: "feed", URL: "feed1", Name: "Feed1"}},
// 					"124": {{Type: "feed", URL: "feed2", Name: "Feed2"}},
// 				},
// 			},
// 			args:    args{feedID: "feed2"},
// 			lens:    map[string]int{"123": 1, "124": 0},
// 			wantErr: false,
// 		},
// 		{
// 			name: "remove unknown",
// 			fields: fields{
// 				Channels: map[string]microsub.Channel{
// 					"123": {UID: "channel1", Name: "Channel 1"},
// 					"124": {UID: "channel2", Name: "Channel 2"},
// 				},
// 				Feeds: map[string][]microsub.Feed{
// 					"123": {{Type: "feed", URL: "feed1", Name: "Feed1"}},
// 					"124": {{Type: "feed", URL: "feed2", Name: "Feed2"}},
// 				},
// 			},
// 			args:    args{feedID: "feed3"},
// 			lens:    map[string]int{"123": 1, "124": 1},
// 			wantErr: false,
// 		},
// 		{
// 			name: "remove from 0 channels",
// 			fields: fields{
// 				Channels: map[string]microsub.Channel{},
// 				Feeds:    map[string][]microsub.Feed{},
// 			},
// 			args:    args{feedID: "feed3"},
// 			lens:    map[string]int{},
// 			wantErr: false,
// 		},
// 		{
// 			name: "remove from multiple channels",
// 			fields: fields{
// 				Channels: map[string]microsub.Channel{
// 					"123": {UID: "channel1", Name: "Channel 1"},
// 					"124": {UID: "channel2", Name: "Channel 2"},
// 				},
// 				Feeds: map[string][]microsub.Feed{
// 					"123": {{Type: "feed", URL: "feed1", Name: "Feed1"}},
// 					"124": {{Type: "feed", URL: "feed1", Name: "Feed1"}},
// 				},
// 			},
// 			args:    args{feedID: "feed1"},
// 			lens:    map[string]int{"123": 0, "124": 0},
// 			wantErr: false,
// 		},
// 	}
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			b := &memoryBackend{
// 				Channels: tt.fields.Channels,
// 				Feeds:    tt.fields.Feeds,
// 			}
// 			if err := b.removeFeed(tt.args.feedID); (err != nil) != tt.wantErr {
// 				t.Errorf("removeFeed() error = %v, wantErr %v", err, tt.wantErr)
// 			}
// 			assert.Len(t, b.Channels, len(tt.lens))
// 			for k, v := range tt.lens {
// 				assert.Len(t, b.Feeds[k], v)
// 			}
// 		})
// 	}
// }
