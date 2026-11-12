package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/pstuifzand/ekster/pkg/client"
)

func TestListFollowsEmptyChannelReturnsArray(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("action") == "follow" {
			// What the server answered before FollowGetList stopped returning
			// a nil slice: valid JSON, but null where callers expect an array.
			_, _ = w.Write([]byte(`{"items":null}`))
			return
		}
		_, _ = w.Write([]byte(`{"channels":[]}`))
	}))
	defer upstream.Close()

	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &client.Client{MicrosubEndpoint: endpoint, Token: "test-token"}

	server := newMCPServer(c)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_follows",
		Arguments: map[string]any{"channel": "empty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool call returned error: %#v", result.Content)
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent type = %T", result.StructuredContent)
	}
	feeds, ok := structured["feeds"].([]any)
	if !ok {
		t.Fatalf("feeds = %#v, want an array", structured["feeds"])
	}
	if len(feeds) != 0 {
		t.Fatalf("feeds = %#v, want an empty array", feeds)
	}
}

func TestMCPToolsExposeSchemasAndStructuredResults(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("missing Microsub bearer token")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"channels":[{"uid":"home","name":"Home","unread":3}]}`))
	}))
	defer upstream.Close()
	endpoint, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	c := &client.Client{MicrosubEndpoint: endpoint, Token: "test-token"}

	server := newMCPServer(c)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	clientSession, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "1"}, nil).Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Tools) != 12 {
		t.Fatalf("got %d tools, want 12", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		if tool.InputSchema == nil {
			t.Errorf("tool %q has no inputSchema", tool.Name)
		}
		if tool.OutputSchema == nil {
			t.Errorf("tool %q has no outputSchema", tool.Name)
		}
	}

	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_channels", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("tool call returned error: %#v", result.Content)
	}
	if result.StructuredContent == nil {
		t.Fatal("missing structuredContent")
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("structuredContent type = %T", result.StructuredContent)
	}
	channels, ok := structured["channels"].([]any)
	if !ok || len(channels) != 1 {
		t.Fatalf("unexpected structured channels: %#v", structured["channels"])
	}
}
