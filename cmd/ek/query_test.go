package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pstuifzand/ekster/pkg/client"
)

func TestQueryOptions(t *testing.T) {
	o, err := parseQueryOptions([]string{"term", "home", "-author", "a", "-author", "b", "-category", "tag", "-kind", "reply", "-source", "feed", "-before", "b", "-after", "a", "-limit", "5"})
	if err != nil || o.Channel != "home" || o.Query != "term" || !reflect.DeepEqual(o.Author, []string{"a", "b"}) || o.Limit != 5 || o.Before != "b" || o.After != "a" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, err := parseQueryOptions([]string{"-kind", "photo"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{}, {"-limit", "0"}, {"-bad", "x"}, {"-kind"}} {
		if _, err := parseQueryOptions(args); err == nil {
			t.Errorf("accepted %v", args)
		}
	}
}

func TestQueryCommandSendsFilters(t *testing.T) {
	var got url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form
		_, _ = w.Write([]byte(`{"items":[],"paging":{"before":"new","after":"old"}}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	performCommands(&client.Client{MicrosubEndpoint: u}, []string{"query", "-kind", "photo", "-author", "a", "-author", "b", "-limit", "5"})
	if got.Get("action") != "search" || got.Get("channel") != "global" || got.Get("limit") != "5" || !reflect.DeepEqual(got["author"], []string{"a", "b"}) || got.Get("kind") != "photo" {
		t.Fatalf("form: %v", got)
	}
}

func TestMCPSearchOptions(t *testing.T) {
	var got url.Values
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[],"paging":{"before":"new","after":"old"}}`))
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	server := newMCPServer(&client.Client{MicrosubEndpoint: u})
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "search_items", Arguments: map[string]any{"author": []string{"a", "b"}, "kind": []string{"photo"}, "limit": 3, "after": "previous"}})
	if err != nil || result.IsError {
		t.Fatalf("%+v %v", result, err)
	}
	if !reflect.DeepEqual(got["author"], []string{"a", "b"}) || got.Get("channel") != "global" || got.Get("after") != "previous" || got.Get("limit") != "3" {
		t.Fatalf("form: %v", got)
	}
	output := result.StructuredContent.(map[string]any)
	paging := output["paging"].(map[string]any)
	if paging["before"] != "new" || paging["after"] != "old" {
		t.Fatalf("paging: %v", paging)
	}
}
