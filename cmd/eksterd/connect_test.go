package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// The identity behind a feed is its origin: a token for https://p83.nl/alerts is
// a token for p83.nl, and the login is started there.
func TestOriginOf(t *testing.T) {
	for in, want := range map[string]string{
		"https://p83.nl/alerts":               "https://p83.nl/",
		"https://p83.nl/alerts?severity=page": "https://p83.nl/",
		"http://localhost:9095/alerts/x.html": "http://localhost:9095/",
		"https://p83.nl/alerts/6e5df1f5.html": "https://p83.nl/",
	} {
		got, err := originOf(in)
		if err != nil || got != want {
			t.Errorf("originOf(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"ftp://p83.nl/x", "not a url", "https://"} {
		if got, err := originOf(bad); err == nil {
			t.Errorf("originOf(%q) = %q, want an error", bad, got)
		}
	}
}

// A token goes to its own host and nowhere else: not to another host in the same
// channel, not to a feed that is a different site with the same path.
func TestTokenForFeedStaysOnItsHost(t *testing.T) {
	setting := channelSetting{FeedToken: "tok", FeedMe: "https://p83.nl/"}

	if got := tokenForFeed(setting, "https://p83.nl/alerts?severity=page"); got != "tok" {
		t.Errorf("same host = %q, want the token", got)
	}
	for _, other := range []string{
		"https://example.com/alerts",
		"https://p83.nl.example.com/alerts",
		"http://p83.nl/alerts", // scheme differs, so the host is not the same site
	} {
		if got := tokenForFeed(setting, other); got != "tok" && got != "" {
			t.Errorf("%s = %q", other, got)
		}
	}
	if got := tokenForFeed(setting, "https://example.com/alerts"); got != "" {
		t.Errorf("the token was offered to another host: %q", got)
	}
	if got := tokenForFeed(channelSetting{}, "https://p83.nl/alerts"); got != "" {
		t.Errorf("a channel without a token sent %q", got)
	}
}

// Discovery reads the endpoints from the site itself: metadata if it is there,
// the page's link tags otherwise.
func TestDiscoverEndpoints(t *testing.T) {
	var withMeta, withLinks bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			if withLinks {
				fmt.Fprintf(w, `<html><head><link rel="authorization_endpoint" href="%s/auth"><link rel="token_endpoint" href="%s/token"></head></html>`, base, base)
			} else {
				fmt.Fprint(w, `<html><head></head><body>profile</body></html>`)
			}
		case "/.well-known/oauth-authorization-server":
			if !withMeta {
				http.NotFound(w, r)
				return
			}
			fmt.Fprintf(w, `{"authorization_endpoint":"%s/auth","token_endpoint":"%s/token"}`, base, base)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	withMeta, withLinks = true, false
	eps, err := discoverEndpoints(context.Background(), srv.URL+"/")
	if err != nil || !strings.HasSuffix(eps.authorization, "/auth") || !strings.HasSuffix(eps.token, "/token") {
		t.Fatalf("metadata discovery = %+v, %v", eps, err)
	}

	withMeta, withLinks = false, true
	eps, err = discoverEndpoints(context.Background(), srv.URL+"/")
	if err != nil || !strings.HasSuffix(eps.authorization, "/auth") {
		t.Fatalf("link discovery = %+v, %v", eps, err)
	}

	withMeta, withLinks = false, false
	if _, err := discoverEndpoints(context.Background(), srv.URL+"/"); err == nil {
		t.Fatal("a page with no endpoints discovered something")
	}
}

// The fetch carries the token when it has one, and says nothing when it does not.
func TestFeedRequestSendsTheToken(t *testing.T) {
	req, err := feedRequest(context.Background(), "https://p83.nl/alerts", "", "", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q", got)
	}
	req, err = feedRequest(context.Background(), "https://p83.nl/alerts", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("an unauthenticated fetch sent %q", got)
	}
}

func TestHasGrantedScope(t *testing.T) {
	if !hasGrantedScope("alerts:read", "alerts:read") {
		t.Error("exact scope refused")
	}
	if !hasGrantedScope("create alerts:read", "alerts:read") {
		t.Error("scope in a list refused")
	}
	if !hasGrantedScope("all", "alerts:read") {
		t.Error("the all scope should cover it")
	}
	if hasGrantedScope("read create", "alerts:read") {
		t.Error("a bare read was accepted for alerts:read")
	}
}

// The pane has to render in both states: a template that only works once a token
// exists is a page you cannot use to get one.
func microsubChannel(name string) microsub.Channel {
	return microsub.Channel{UID: name, Name: name}
}

func TestChannelPageRendersTheConnectPane(t *testing.T) {
	page := settingsPage{
		Session:        session{Me: "https://p83.nl/", LoggedIn: true},
		CurrentChannel: microsubChannel("alerts"),
		Channels:       []microsub.Channel{microsubChannel("alerts")},
	}
	var buf strings.Builder
	if err := renderPage(&buf, "channel.html", page); err != nil {
		t.Fatalf("render without a token: %v", err)
	}
	if !strings.Contains(buf.String(), "Log in and connect") {
		t.Error("the page offers no way to connect a private feed")
	}
	if strings.Contains(buf.String(), "Disconnect") {
		t.Error("an unconnected channel shows a Disconnect button")
	}

	page.CurrentSetting = channelSetting{
		FeedToken: "sevret-value-that-must-not-be-rendered", FeedMe: "https://p83.nl/", FeedScope: "alerts:read",
		FeedURL: "https://p83.nl/alerts",
	}
	buf.Reset()
	if err := renderPage(&buf, "channel.html", page); err != nil {
		t.Fatalf("render with a token: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Disconnect") || !strings.Contains(out, "alerts:read") {
		t.Error("a connected channel does not show what it is connected to")
	}
	if strings.Contains(out, "sevret-value-that-must-not-be-rendered") {
		t.Error("the token itself was rendered into the page")
	}
}
