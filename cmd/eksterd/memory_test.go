package main

import (
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/gomodule/redigo/redis"
	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/sse"
)

func Test_memoryBackend_ChannelsCreate(t *testing.T) {
	type fields struct {
		hubIncomingBackend hubIncomingBackend
		lock               sync.RWMutex
		Channels           map[string]microsub.Channel
		Feeds              map[string][]microsub.Feed
		Settings           map[string]channelSetting
		NextUID            int
		Me                 string
		TokenEndpoint      string
		AuthEnabled        bool
		ticker             *time.Ticker
		quit               chan struct{}
		broker             *sse.Broker
		pool               *redis.Pool
	}
	type args struct {
		name string
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    microsub.Channel
		wantErr bool
	}{
		{
			name: "Duplicate channel",
			fields: fields{
				hubIncomingBackend: hubIncomingBackend{},
				lock:               sync.RWMutex{},
				Channels: func() map[string]microsub.Channel {
					channels := make(map[string]microsub.Channel)
					channels["1234"] = microsub.Channel{
						UID:  "1234",
						Name: "Test",
						Unread: microsub.Unread{
							Type:        microsub.UnreadCount,
							Unread:      false,
							UnreadCount: 0,
						},
					}
					return channels
				}(),
				Feeds: func() map[string][]microsub.Feed {
					feeds := make(map[string][]microsub.Feed)
					return feeds
				}(),
				Settings:      nil,
				NextUID:       1,
				Me:            "",
				TokenEndpoint: "",
				AuthEnabled:   false,
				ticker:        nil,
				quit:          nil,
				broker:        nil,
				pool:          nil,
			},
			args: args{
				name: "Test",
			},
			want: microsub.Channel{
				UID:  "1234",
				Name: "Test",
				Unread: microsub.Unread{
					Type:        microsub.UnreadCount,
					Unread:      false,
					UnreadCount: 0,
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := &memoryBackend{
				hubIncomingBackend: tt.fields.hubIncomingBackend,
				lock:               tt.fields.lock,
				Channels:           tt.fields.Channels,
				Feeds:              tt.fields.Feeds,
				Settings:           tt.fields.Settings,
				NextUID:            tt.fields.NextUID,
				Me:                 tt.fields.Me,
				TokenEndpoint:      tt.fields.TokenEndpoint,
				AuthEnabled:        tt.fields.AuthEnabled,
				ticker:             tt.fields.ticker,
				quit:               tt.fields.quit,
				broker:             tt.fields.broker,
				pool:               tt.fields.pool,
			}
			got, err := b.ChannelsCreate(tt.args.name)
			if (err != nil) != tt.wantErr {
				t.Errorf("ChannelsCreate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ChannelsCreate() got = %v, want %v", got, tt.want)
			}
		})
	}
}
