/*
 *  Ekster is a microsub server
 *  Copyright (c) 2026 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

package main

import (
	"bytes"
	"io/fs"
	"strings"
	"testing"

	"github.com/pstuifzand/ekster/pkg/microsub"
)

// page fixtures: one logged-in view per page, because every page is rendered
// inside base.html and base.html only draws the menu for a session.
func pageFixtures() map[string]struct {
	data   interface{}
	marker string // text the page's own content must contain
	nav    string // href that must carry aria-current="page"
} {
	sess := session{LoggedIn: true, Me: "https://p83.nl/", UserID: 1}
	channels := []microsub.Channel{{UID: "home", Name: "Home"}}

	return map[string]struct {
		data   interface{}
		marker string
		nav    string
	}{
		"index.html": {
			data:   indexPage{Session: sess, Nav: "home", Baseurl: "https://microsub.p83.nl", MicrosubURL: "https://microsub.p83.nl/microsub/1"},
			marker: "Microsub endpoint",
			nav:    "/",
		},
		"settings.html": {
			data:   settingsPage{Session: sess, Nav: "settings", Channels: channels},
			marker: "UID",
			nav:    "/settings",
		},
		"channel.html": {
			data: settingsPage{
				Session:        sess,
				Nav:            "settings",
				CurrentChannel: microsub.Channel{UID: "home", Name: "Home"},
				Feeds:          []microsub.Feed{{URL: "https://p83.nl/"}},
			},
			marker: "Blocking regex",
			nav:    "/settings",
		},
		"sources.html": {
			data: sourcesPage{
				Session: sess,
				Nav:     "sources",
				Sources: []sourceView{{
					ID: 1, Name: "Gmail", ChannelUID: "home", ChannelName: "Home",
					Token: "abcdef123456", TokenHint: "abcdef…",
					Endpoint: "https://microsub.p83.nl/micropub",
				}},
				Channels: channels,
			},
			marker: "Micropub sources",
			nav:    "/settings/sources",
		},
		"status.html": {
			data:   statusPage{Session: sess, Nav: "status"},
			marker: "Feed status",
			nav:    "/status",
		},
		"logs.html": {
			data:   logsPage{Session: sess, Nav: "logs"},
			marker: "journal",
			nav:    "/logs",
		},
		"auth.html": {
			data: authPage{
				Session: sess, ClientID: "https://reader.p83.nl/",
				RedirectURI: "https://reader.p83.nl/callback", Scope: "read",
				State: "state", Channels: channels,
			},
			marker: "Authorize an app",
			nav:    "",
		},
	}
}

// TestEveryFragmentPageRendersInsideTheChrome is the guard for the shape the
// admin pages now have: a {{define "title"}} and {{define "content"}} pair
// ending in {{template "base.html" .}}. A page that drops one of the defines
// does not fail — base.html's empty default stands in, the page renders with no
// body or a generic <title>, and only a person looking at it would notice.
func TestEveryFragmentPageRendersInsideTheChrome(t *testing.T) {
	entries, err := fs.ReadDir(templates, "templates")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}

	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".html") || name == "base.html" || name == "item.html" {
			continue
		}
		seen[name] = true

		source, err := templates.ReadFile("templates/" + name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, block := range []string{`{{define "title"}}`, `{{define "content"}}`, `{{template "base.html" .}}`} {
			if !strings.Contains(string(source), block) {
				t.Errorf("%s does not contain %s; it would render with base.html's empty default", name, block)
			}
		}
	}

	for name, fixture := range pageFixtures() {
		if !seen[name] {
			t.Errorf("%s has a fixture but is not in the templates directory", name)
			continue
		}

		var body bytes.Buffer
		if err := renderPage(&body, name, fixture.data); err != nil {
			t.Errorf("render %s: %v", name, err)
			continue
		}
		out := body.String()

		if !strings.Contains(out, `class="site-header"`) {
			t.Errorf("%s did not render inside the shared chrome", name)
		}
		if !strings.Contains(out, fixture.marker) {
			t.Errorf("%s rendered without its content: %q missing", name, fixture.marker)
		}
		if !strings.Contains(out, "<title>") || strings.Contains(out, "<title></title>") {
			t.Errorf("%s rendered without a title", name)
		}
		if name != "index.html" && strings.Contains(out, "<title>Ekster</title>") {
			t.Errorf("%s fell back to the default title instead of defining its own", name)
		}
		if fixture.nav != "" {
			current := `href="` + fixture.nav + `" aria-current="page"`
			if !strings.Contains(out, current) {
				t.Errorf("%s does not mark %s as the current page", name, fixture.nav)
			}
		}
	}
}

// TestSignedOutHomepageIsASignInForm covers the branch a visitor sees first: no
// session means no menu, and the page has to offer a labelled field rather than
// an input under a heading.
func TestSignedOutHomepageIsASignInForm(t *testing.T) {
	var body bytes.Buffer
	err := renderPage(&body, "index.html", indexPage{MicrosubURL: "https://microsub.p83.nl/microsub/1"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := body.String()

	for _, want := range []string{`id="url"`, `action="/session"`, "Sign in"} {
		if !strings.Contains(out, want) {
			t.Errorf("signed-out homepage is missing %q", want)
		}
	}
	if strings.Contains(out, `id="site-nav"`) {
		t.Error("signed-out homepage renders the menu, which needs a session to mean anything")
	}
}

// TestSourceTokenIsCopiedNotShown pins the sources cell shape: the page shows a
// hint and a Copy button that carries the whole token, so the secret is not
// rendered as a readable input.
func TestSourceTokenIsCopiedNotShown(t *testing.T) {
	var body bytes.Buffer
	err := renderPage(&body, "sources.html", sourcesPage{
		Session: session{LoggedIn: true, Me: "https://p83.nl/"},
		Nav:     "sources",
		Sources: []sourceView{{ID: 1, Name: "Gmail", Token: "abcdef123456", TokenHint: "abcdef…"}},
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	out := body.String()

	if !strings.Contains(out, `data-copy="abcdef123456"`) {
		t.Error("the Copy button does not carry the token")
	}
	if strings.Contains(out, `type="password"`) {
		t.Error("the token is still rendered as a password input")
	}
}

func TestTokenHint(t *testing.T) {
	cases := map[string]string{
		"abcdef123456": "abcdef…",
		"abcdef":       "abcdef",
		"abc":          "abc",
		"":             "",
	}
	for token, want := range cases {
		if got := tokenHint(token); got != want {
			t.Errorf("tokenHint(%q) = %q, want %q", token, got, want)
		}
	}
}
