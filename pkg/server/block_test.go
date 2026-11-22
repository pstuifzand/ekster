package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

type blockBackend struct {
	NullBackend
	channel string
	cards   []microsub.Card
	lastURL string
	action  string
}

func (b *blockBackend) BlockGetList(_ context.Context, channel string) ([]microsub.Card, error) {
	b.channel = channel
	if channel == "foreign" {
		return nil, microsub.ErrNotFound
	}
	if b.cards == nil {
		return []microsub.Card{}, nil
	}
	return b.cards, nil
}

func (b *blockBackend) BlockURL(_ context.Context, channel, url string) error {
	if channel == "foreign" {
		return microsub.ErrNotFound
	}
	if err := microsub.ValidateProfileURL(url); err != nil {
		return err
	}
	b.channel, b.lastURL, b.action = channel, url, "block"
	return nil
}

func (b *blockBackend) UnblockURL(_ context.Context, channel, url string) error {
	if channel == "foreign" {
		return microsub.ErrNotFound
	}
	if err := microsub.ValidateProfileURL(url); err != nil {
		return err
	}
	b.channel, b.lastURL, b.action = channel, url, "unblock"
	return nil
}

func TestServerBlockDispatchAndClient(t *testing.T) {
	backend := &blockBackend{cards: []microsub.Card{
		{Type: "card", URL: "https://blocked.example/", Name: "Blocked Person"},
	}}
	handler, _ := NewMicrosubHandler(backend)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	endpoint, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &client.Client{MicrosubEndpoint: endpoint}

	cards, err := c.BlockGetList(context.Background(), "channel")
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 1 || cards[0].URL != "https://blocked.example/" || cards[0].Name != "Blocked Person" || backend.channel != "channel" {
		t.Fatalf("cards=%v channel=%q", cards, backend.channel)
	}
	if err := c.BlockURL(context.Background(), "channel", "https://block.example/"); err != nil {
		t.Fatal(err)
	}
	if backend.action != "block" || backend.channel != "channel" || backend.lastURL != "https://block.example/" {
		t.Fatalf("action=%q channel=%q url=%q", backend.action, backend.channel, backend.lastURL)
	}
	if err := c.UnblockURL(context.Background(), "", "https://block.example/"); err != nil {
		t.Fatal(err)
	}
	if backend.action != "unblock" || backend.channel != "" || backend.lastURL != "https://block.example/" {
		t.Fatalf("action=%q channel=%q url=%q", backend.action, backend.channel, backend.lastURL)
	}
}

func TestServerBlockErrors(t *testing.T) {
	backend := &blockBackend{}
	handler, _ := NewMicrosubHandler(backend)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	get := func(rawQuery string) *http.Response {
		res, err := http.Get(srv.URL + "/?" + rawQuery)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	post := func(form url.Values) *http.Response {
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/", strings.NewReader(form.Encode()))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	if res := get("action=block&channel=foreign"); res.StatusCode != http.StatusNotFound {
		res.Body.Close()
		t.Fatalf("foreign GET status=%d, want 404", res.StatusCode)
	} else {
		res.Body.Close()
	}
	if res := post(url.Values{"action": {"block"}, "channel": {"foreign"}, "url": {"https://block.example/"}}); res.StatusCode != http.StatusNotFound {
		res.Body.Close()
		t.Fatalf("foreign POST status=%d, want 404", res.StatusCode)
	} else {
		res.Body.Close()
	}
	if res := post(url.Values{"action": {"block"}, "channel": {"home"}, "url": {"not a url"}}); res.StatusCode != http.StatusBadRequest {
		res.Body.Close()
		t.Fatalf("invalid URL status=%d, want 400", res.StatusCode)
	} else {
		res.Body.Close()
	}

	res := get("action=block&channel=home")
	defer res.Body.Close()
	var body map[string]json.RawMessage
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if string(body["items"]) != "[]" {
		t.Fatalf("empty block list encoded as %s, want []", body["items"])
	}
}
