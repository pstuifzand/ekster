package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

type orderBackend struct {
	NullBackend
	got []string
	err error
}

func (b *orderBackend) ChannelsOrder(_ context.Context, uids []string) error {
	b.got = uids
	return b.err
}
func TestChannelOrderHTTPAndClient(t *testing.T) {
	b := &orderBackend{}
	h, _ := NewMicrosubHandler(b)
	s := httptest.NewServer(h)
	defer s.Close()
	endpoint, _ := url.Parse(s.URL + "?existing=value")
	c := &client.Client{MicrosubEndpoint: endpoint}
	want := []string{"d", "a", "c", "g"}
	if err := c.ChannelsOrder(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.got, want) {
		t.Fatalf("got %v", b.got)
	}
	b.err = microsub.ErrInvalidChannelOrder
	if err := c.ChannelsOrder(context.Background(), want); err == nil {
		t.Fatal("expected error")
	}
	resp, err := http.PostForm(s.URL, url.Values{"action": {"channels"}, "method": {"order"}, "channels[]": {"a", "a"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
	b.err = errors.New("db failure")
	resp2, err := http.PostForm(s.URL, url.Values{"action": {"channels"}, "method": {"order"}})
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status %d", resp2.StatusCode)
	}
}
