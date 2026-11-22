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

func TestMuteCLIAndMCP(t *testing.T) {
	var action, channel, mutedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action, channel, mutedURL = r.FormValue("action"), r.FormValue("channel"), r.FormValue("url")
		if action == "mute" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"items":[{"type":"card","url":"https://muted.example/"}]}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	c := &client.Client{MicrosubEndpoint: endpoint}

	performCommands(c, []string{"mute", "https://mute.example/"})
	if action != "mute" || channel != "" || mutedURL != "https://mute.example/" {
		t.Fatalf("CLI action=%q channel=%q url=%q", action, channel, mutedURL)
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := newMCPServer(c).Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_mutes", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("list_mutes: result=%v err=%v", result, err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || !reflect.DeepEqual(structured["urls"], []any{"https://muted.example/"}) {
		t.Fatalf("list_mutes result=%#v", result.StructuredContent)
	}
	result, err = clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "unmute_url", Arguments: map[string]any{"channel": "one", "url": "https://mute.example/"}})
	if err != nil || result.IsError {
		t.Fatalf("unmute_url: result=%v err=%v", result, err)
	}
	if action != "unmute" || channel != "one" || mutedURL != "https://mute.example/" {
		t.Fatalf("MCP action=%q channel=%q url=%q", action, channel, mutedURL)
	}
}
