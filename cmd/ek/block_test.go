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

func TestBlockCLIAndMCP(t *testing.T) {
	var action, channel, blockedURL string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action, channel, blockedURL = r.FormValue("action"), r.FormValue("channel"), r.FormValue("url")
		if action == "block" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"items":[{"type":"card","url":"https://blocked.example/","name":"Blocked"}]}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	defer upstream.Close()
	endpoint, _ := url.Parse(upstream.URL)
	c := &client.Client{MicrosubEndpoint: endpoint}

	performCommands(c, []string{"block", "https://block.example/"})
	if action != "block" || channel != "" || blockedURL != "https://block.example/" {
		t.Fatalf("CLI action=%q channel=%q url=%q", action, channel, blockedURL)
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

	result, err := clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "list_blocks", Arguments: map[string]any{}})
	if err != nil || result.IsError {
		t.Fatalf("list_blocks: result=%v err=%v", result, err)
	}
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok || !reflect.DeepEqual(structured["urls"], []any{"https://blocked.example/"}) {
		t.Fatalf("list_blocks result=%#v", result.StructuredContent)
	}
	result, err = clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "block_url", Arguments: map[string]any{"channel": "one", "url": "https://block.example/"}})
	if err != nil || result.IsError {
		t.Fatalf("block_url: result=%v err=%v", result, err)
	}
	if action != "block" || channel != "one" || blockedURL != "https://block.example/" {
		t.Fatalf("MCP block action=%q channel=%q url=%q", action, channel, blockedURL)
	}
	result, err = clientSession.CallTool(context.Background(), &mcp.CallToolParams{Name: "unblock_url", Arguments: map[string]any{"channel": "one", "url": "https://block.example/"}})
	if err != nil || result.IsError {
		t.Fatalf("unblock_url: result=%v err=%v", result, err)
	}
	if action != "unblock" || channel != "one" || blockedURL != "https://block.example/" {
		t.Fatalf("MCP unblock action=%q channel=%q url=%q", action, channel, blockedURL)
	}
}
