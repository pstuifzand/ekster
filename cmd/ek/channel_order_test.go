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

func TestChannelOrderCLIAndMCP(t *testing.T) {
	var got []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.FormValue("action") != "channels" || r.FormValue("method") != "order" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
		}
		got = append([]string(nil), r.Form["channels[]"]...)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	c := &client.Client{MicrosubEndpoint: endpoint}
	performCommands(c, []string{"channels", "-order", "b", "a"})
	if !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Fatalf("CLI: %v", got)
	}
	st, ct := mcp.NewInMemoryTransports()
	session, err := newMCPServer(c).Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	result, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "order_channels", Arguments: map[string]any{"channels": []string{"a", "b"}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("MCP: %v", result.Content)
	}
	if !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Fatalf("MCP: %v", got)
	}
}
