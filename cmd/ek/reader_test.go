package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/client"
)

func TestReaderMicrosubProxyUsesSavedEndpointAndToken(t *testing.T) {
	var gotAuthorization, gotQuery, gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		gotQuery = r.URL.RawQuery
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()
	endpoint, err := url.Parse(upstream.URL + "/microsub")
	if err != nil {
		t.Fatal(err)
	}
	s := &readerServer{client: client.Client{MicrosubEndpoint: endpoint, Token: "secret"}}
	req := httptest.NewRequest(http.MethodPost, "/_ek/api/microsub?action=timeline&method=mark_read", strings.NewReader("entry%5B%5D=abc"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r := httptest.NewRecorder()
	s.microsub(r, req)
	if r.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", r.Code)
	}
	if gotAuthorization != "Bearer secret" {
		t.Fatalf("authorization = %q", gotAuthorization)
	}
	if gotQuery != "action=timeline&method=mark_read" {
		t.Fatalf("query = %q", gotQuery)
	}
	if gotBody != "entry%5B%5D=abc" {
		t.Fatalf("body = %q", gotBody)
	}
}

func TestLocalOnlyRequiresLoopbackAndExpectedHost(t *testing.T) {
	h := localOnly(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }), "127.0.0.1:1234")
	for _, tc := range []struct {
		name, remote, host string
		want               int
	}{
		{"local", "127.0.0.1:5555", "127.0.0.1:1234", http.StatusOK},
		{"wrong host", "127.0.0.1:5555", "attacker.example", http.StatusForbidden},
		{"remote", "192.0.2.1:5555", "127.0.0.1:1234", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/", nil)
			req.RemoteAddr = tc.remote
			r := httptest.NewRecorder()
			h.ServeHTTP(r, req)
			if r.Code != tc.want {
				t.Fatalf("status = %d, want %d", r.Code, tc.want)
			}
		})
	}
}
