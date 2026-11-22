package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
)

type muteBackend struct {
	NullBackend
	channel string
	muted   []string
	lastURL string
	action  string
}

func (b *muteBackend) MuteGetList(_ context.Context, channel string) ([]string, error) {
	b.channel = channel
	return b.muted, nil
}

func (b *muteBackend) MuteURL(_ context.Context, channel, url string) error {
	b.channel, b.lastURL, b.action = channel, url, "mute"
	return nil
}

func (b *muteBackend) UnmuteURL(_ context.Context, channel, url string) error {
	b.channel, b.lastURL, b.action = channel, url, "unmute"
	return nil
}

func TestServerMuteDispatchAndClient(t *testing.T) {
	backend := &muteBackend{muted: []string{"https://muted.example/"}}
	handler, _ := NewMicrosubHandler(backend)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	endpoint, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &client.Client{MicrosubEndpoint: endpoint}

	urls, err := c.MuteGetList(context.Background(), "channel")
	if err != nil {
		t.Fatal(err)
	}
	if len(urls) != 1 || urls[0] != "https://muted.example/" || backend.channel != "channel" {
		t.Fatalf("urls=%v channel=%q", urls, backend.channel)
	}
	if err := c.MuteURL(context.Background(), "channel", "https://mute.example/"); err != nil {
		t.Fatal(err)
	}
	if backend.action != "mute" || backend.channel != "channel" || backend.lastURL != "https://mute.example/" {
		t.Fatalf("action=%q channel=%q url=%q", backend.action, backend.channel, backend.lastURL)
	}
	if err := c.UnmuteURL(context.Background(), "", "https://mute.example/"); err != nil {
		t.Fatal(err)
	}
	if backend.action != "unmute" || backend.channel != "" || backend.lastURL != "https://mute.example/" {
		t.Fatalf("action=%q channel=%q url=%q", backend.action, backend.channel, backend.lastURL)
	}
}
