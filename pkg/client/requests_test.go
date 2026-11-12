package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// MarkRead used to send entry[] in the body without a Content-Type, and the server's
// ParseForm ignores a body it cannot classify: mark_read answered 200 and marked
// nothing, and the command line reported the number of entries it had sent.
func TestMarkReadSendsParseableForm(t *testing.T) {
	var gotChannel, gotMethod string
	var gotEntries []string
	var gotContentType string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotChannel = r.URL.Query().Get("channel")
		gotMethod = r.URL.Query().Get("method")
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotEntries = r.Form["entry[]"]
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	endpoint, err := url.Parse(srv.URL + "/microsub/1")
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{MicrosubEndpoint: endpoint, Token: "tok"}

	if err := c.MarkRead(context.Background(), "chan", []string{"a", "b"}); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	if gotContentType != "application/x-www-form-urlencoded" {
		t.Errorf("content type = %q, want application/x-www-form-urlencoded", gotContentType)
	}
	if gotChannel != "chan" || gotMethod != "mark_read" {
		t.Errorf("channel/method = %q/%q", gotChannel, gotMethod)
	}
	if len(gotEntries) != 2 || gotEntries[0] != "a" || gotEntries[1] != "b" {
		t.Errorf("entries = %v, want [a b]", gotEntries)
	}
}

// TestItemSearchNoResultsIsEmptyList covers the response shape for a search
// that matches nothing. A nil slice encodes as null, which fails callers that
// validate an array output (the MCP search_items tool does).
func TestItemSearchNoResultsIsEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":null,"query":"nothing"}`))
	}))
	defer srv.Close()

	endpoint, err := url.Parse(srv.URL + "/microsub/1")
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{MicrosubEndpoint: endpoint, Token: "tok"}

	items, err := c.ItemSearch(context.Background(), "global", "nothing")
	if err != nil {
		t.Fatalf("ItemSearch: %v", err)
	}
	if items == nil {
		t.Fatal("ItemSearch returned nil, want an empty slice")
	}
	if len(items) != 0 {
		t.Fatalf("ItemSearch returned %d items, want 0", len(items))
	}
}

func TestPostFormRequestReturnsTransportError(t *testing.T) {
	// A closed server used to panic here: the response was dereferenced before err.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	endpoint, _ := url.Parse(srv.URL + "/microsub/1")
	srv.Close()

	c := &Client{MicrosubEndpoint: endpoint, Token: "tok"}
	if err := c.MarkRead(context.Background(), "chan", []string{"a"}); err == nil {
		t.Fatal("expected an error from a closed server")
	}
}

func TestPostFormRequestReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	endpoint, _ := url.Parse(srv.URL + "/microsub/1")
	c := &Client{MicrosubEndpoint: endpoint, Token: "tok"}
	err := c.MarkRead(context.Background(), "chan", []string{"a"})
	if err == nil {
		t.Fatal("expected an error for a 500")
	}
}
