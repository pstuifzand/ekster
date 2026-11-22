package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

type searchOptionsBackend struct {
	NullBackend
	got microsub.ItemSearchOptions
	err error
}

func (b *searchOptionsBackend) ItemSearchWithOptions(_ context.Context, o microsub.ItemSearchOptions) (microsub.Timeline, error) {
	b.got = o
	return microsub.Timeline{Items: []microsub.Item{{ID: "one"}}, Paging: microsub.Pagination{Before: "new", After: "old"}}, b.err
}
func TestSearchOptionsHTTPAndClient(t *testing.T) {
	b := &searchOptionsBackend{}
	h, _ := NewMicrosubHandler(b)
	s := httptest.NewServer(h)
	defer s.Close()
	endpoint, _ := url.Parse(s.URL + "?tenant=x")
	c := &client.Client{MicrosubEndpoint: endpoint}
	want := microsub.ItemSearchOptions{Channel: "home", Query: "hello", Author: []string{"a", "b"}, Category: []string{"x", "y"}, Kind: []string{"reply", "photo"}, Source: []string{"one", "two"}, Before: "new", After: "old", Limit: 3}
	page, err := c.ItemSearchWithOptions(context.Background(), want)
	if err != nil || !reflect.DeepEqual(b.got, want) || page.Paging.After != "old" {
		t.Fatalf("got=%+v page=%+v err=%v", b.got, page, err)
	}
	params := url.Values{"action": {"search"}, "channel": {"home"}, "query": {"hello"}, "author": {"a", "b"}, "category": {"x", "y"}, "kind": {"reply", "photo"}, "source": {"one", "two"}, "before": {"new"}, "after": {"old"}, "limit": {"3"}}
	r, err := http.Get(s.URL + "?" + params.Encode())
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 200 || !reflect.DeepEqual(b.got, want) {
		t.Fatalf("GET %d %+v", r.StatusCode, b.got)
	}
	r, err = http.Post(s.URL+"?action=search&query=wrong&channel=wrong", "application/x-www-form-urlencoded", strings.NewReader("query=right&channel=home"))
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if b.got.Query != "right" || b.got.Channel != "home" {
		t.Fatalf("body precedence: %+v", b.got)
	}
	b.err = errors.New("database unavailable")
	if _, err := c.ItemSearchWithOptions(context.Background(), want); err == nil {
		t.Fatal("backend error hidden")
	}
	for _, q := range []string{"channel=home", "channel=&query=x", "channel=home&query=x&limit=0", "channel=home&query=x&limit=1&limit=2", "channel=home&category="} {
		r, err := http.Get(s.URL + "?action=search&" + q)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 {
			t.Errorf("%s: %d", q, r.StatusCode)
		}
	}
}
