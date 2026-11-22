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

func TestMarkUnreadCLIAndMCP(t *testing.T) {
	var got []string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.FormValue("action") == "channels" {
			w.Write([]byte(`{"channels":[{"uid":"one","name":"One"}]}`))
			return
		}
		if r.Method != "POST" || r.FormValue("action") != "timeline" || r.FormValue("method") != "mark_unread" || r.FormValue("channel") != "one" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		got = append([]string(nil), r.Form["entry[]"]...)
		w.Write([]byte(`[]`))
	}))
	defer s.Close()
	u, _ := url.Parse(s.URL)
	c := &client.Client{MicrosubEndpoint: u}
	performCommands(c, []string{"mark-unread", "One", "a", "b"})
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("CLI: %v", got)
	}
	st, ct := mcp.NewInMemoryTransports()
	ss, err := newMCPServer(c).Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "mark_unread", Arguments: map[string]any{"channel": "one", "entries": []string{"b"}}})
	if err != nil || result.IsError {
		t.Fatalf("MCP: %v %v", result, err)
	}
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("MCP: %v", got)
	}
}
