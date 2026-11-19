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
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gomodule/redigo/redis"
)

// A private feed needs a credential, and the credential is minted by the site
// that serves the feed: log in there once, and the token comes back here. This is
// the reader's half of that - a page, not a command, because the person is
// already looking at the channel they are about to connect.
const (
	feedConnectLifetime = 10 * time.Minute
	// feedConnectScope is the narrowest scope that lets a reader fetch the alert
	// page. Requesting it also means the consent screen can say what this reader
	// wants instead of asking for everything.
	feedConnectScope = "alerts:read"
)

type feedLogin struct {
	channel       string
	feed          string
	verifier      string
	authorization string
	tokenEndpoint string
	me            string // the origin the login is for
	expires       time.Time
}

var feedLogins = struct {
	mu sync.Mutex
	m  map[string]feedLogin
}{m: map[string]feedLogin{}}

var ekLinkTag = regexp.MustCompile(`(?is)<link\b[^>]*>`)
var ekAttr = regexp.MustCompile(`(?is)\b(rel|href)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)

func ekLinkRels(page, rel string) []string {
	var out []string
	for _, tag := range ekLinkTag.FindAllString(page, -1) {
		attrs := map[string]string{}
		for _, m := range ekAttr.FindAllStringSubmatch(tag, -1) {
			attrs[strings.ToLower(m[1])] = m[3] + m[4] + m[5]
		}
		for _, r := range strings.Fields(strings.ToLower(attrs["rel"])) {
			if r == rel && attrs["href"] != "" {
				out = append(out, attrs["href"])
			}
		}
	}
	return out
}

// originOf is the identity a feed belongs to: the site, not the item URL. A
// token for https://p83.nl/alerts is a token for p83.nl.
func originOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", fmt.Errorf("%s is not an http(s) URL", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%s has no host", raw)
	}
	return u.Scheme + "://" + u.Host + "/", nil
}

type discoveredEndpoints struct {
	authorization string
	token         string
}

// discoverEndpoints reads the endpoints from the identity itself: metadata first,
// then the page's link tags. Nothing about the site is configured here, which is
// what lets this work for any IndieAuth server.
func discoverEndpoints(ctx context.Context, me string) (discoveredEndpoints, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, me, nil)
	if err != nil {
		return discoveredEndpoints{}, err
	}
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "text/html, application/json;q=0.9")
	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return discoveredEndpoints{}, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 512<<10))
	if err != nil {
		return discoveredEndpoints{}, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return discoveredEndpoints{}, fmt.Errorf("%s answered %s", me, res.Status)
	}
	page := string(body)
	base, err := url.Parse(me)
	if err != nil {
		return discoveredEndpoints{}, err
	}
	resolve := func(href string) string {
		u, err := url.Parse(href)
		if err != nil {
			return href
		}
		return base.ResolveReference(u).String()
	}

	var meta struct {
		Authorization string `json:"authorization_endpoint"`
		Token         string `json:"token_endpoint"`
	}
	candidates := []string{base.Scheme + "://" + base.Host + "/.well-known/oauth-authorization-server"}
	for _, href := range ekLinkRels(page, "indieauth-metadata") {
		candidates = append([]string{resolve(href)}, candidates...)
	}
	for _, candidate := range candidates {
		raw, err := fetchJSON(ctx, candidate)
		if err != nil {
			continue
		}
		if json.Unmarshal(raw, &meta) == nil && meta.Authorization != "" {
			break
		}
		meta.Authorization, meta.Token = "", ""
	}

	out := discoveredEndpoints{authorization: meta.Authorization, token: meta.Token}
	if out.authorization == "" {
		if hrefs := ekLinkRels(page, "authorization_endpoint"); len(hrefs) > 0 {
			out.authorization = resolve(hrefs[0])
		}
	}
	if out.token == "" {
		if hrefs := ekLinkRels(page, "token_endpoint"); len(hrefs) > 0 {
			out.token = resolve(hrefs[0])
		}
	}
	if out.authorization == "" || out.token == "" {
		return discoveredEndpoints{}, fmt.Errorf("%s does not name an authorization and token endpoint", me)
	}
	return out, nil
}

func fetchJSON(ctx context.Context, target string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/json")
	client := http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", target, res.Status)
	}
	return io.ReadAll(io.LimitReader(res.Body, 64<<10))
}

// requireSession is the guard the three connect handlers share. The channel page
// already refuses to render without a session; going through the flow must not be
// a way in.
func (h *mainHandler) requireSession(w http.ResponseWriter, r *http.Request, conn redis.Conn) (session, bool) {
	cookie, err := r.Cookie("session")
	if err != nil {
		http.Redirect(w, r, "/", http.StatusFound)
		return session{}, false
	}
	sess, err := loadSession(cookie.Value, conn)
	if err != nil {
		slog.ErrorContext(r.Context(), "could not load session", "path", r.URL.Path, "error", err)
		http.Redirect(w, r, "/", http.StatusFound)
		return session{}, false
	}
	if !isLoggedIn(h.Backend, &sess) {
		http.Redirect(w, r, "/", http.StatusFound)
		return session{}, false
	}
	return sess, true
}

// handleConnectStart sends the browser to the feed's own site to log in.
func (h *mainHandler) handleConnectStart(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	if _, ok := h.requireSession(w, r, conn); !ok {
		return
	}
	uid := r.URL.Query().Get("uid")
	feed := strings.TrimSpace(r.URL.Query().Get("feed"))
	if uid == "" || feed == "" {
		http.Redirect(w, r, "/settings", http.StatusFound)
		return
	}
	if _, err := url.Parse(feed); err != nil {
		h.connectFail(w, r, uid, "that feed URL is not a URL")
		return
	}

	me, err := originOf(feed)
	if err != nil {
		h.connectFail(w, r, uid, err.Error())
		return
	}
	eps, err := discoverEndpoints(r.Context(), me)
	if err != nil {
		h.connectFail(w, r, uid, err.Error())
		return
	}

	state := ekRandomToken(24)
	verifier := ekRandomToken(48)
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	feedLogins.mu.Lock()
	for k, v := range feedLogins.m {
		if time.Now().After(v.expires) {
			delete(feedLogins.m, k)
		}
	}
	feedLogins.m[state] = feedLogin{
		channel:       uid,
		feed:          feed,
		verifier:      verifier,
		authorization: eps.authorization,
		tokenEndpoint: eps.token,
		me:            me,
		expires:       time.Now().Add(feedConnectLifetime),
	}
	feedLogins.mu.Unlock()

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {strings.TrimRight(h.BaseURL, "/") + "/"},
		"redirect_uri":          {strings.TrimRight(h.BaseURL, "/") + "/settings/connect/callback"},
		"state":                 {state},
		"scope":                 {feedConnectScope},
		"me":                    {me},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	http.Redirect(w, r, eps.authorization+"?"+q.Encode(), http.StatusFound)
}

// handleConnectCallback stores the token on the channel it was started from.
func (h *mainHandler) handleConnectCallback(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	if _, ok := h.requireSession(w, r, conn); !ok {
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	feedLogins.mu.Lock()
	p, ok := feedLogins.m[state]
	delete(feedLogins.m, state)
	feedLogins.mu.Unlock()
	if !ok || time.Now().After(p.expires) {
		http.Error(w, "that login expired; start again from the channel page", http.StatusBadRequest)
		return
	}
	if code == "" {
		if e := r.URL.Query().Get("error"); e != "" {
			h.connectFail(w, r, p.channel, "the authorization server refused: "+e)
			return
		}
		h.connectFail(w, r, p.channel, "the callback carried no code")
		return
	}

	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"client_id":     {strings.TrimRight(h.BaseURL, "/") + "/"},
		"redirect_uri":  {strings.TrimRight(h.BaseURL, "/") + "/settings/connect/callback"},
		"code_verifier": {p.verifier},
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, p.tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		h.connectFail(w, r, p.channel, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent())
	res, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		h.connectFail(w, r, p.channel, "could not reach the token endpoint: "+err.Error())
		return
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 8192))
	if res.StatusCode != http.StatusOK {
		h.connectFail(w, r, p.channel, fmt.Sprintf("the token endpoint answered %s", res.Status))
		return
	}
	var tok authTokenResponse
	if err := json.Unmarshal(body, &tok); err != nil || tok.AccessToken == "" {
		h.connectFail(w, r, p.channel, "the token endpoint did not answer with a token")
		return
	}
	// The token has to belong to the site the feed is on, or it would be sent to
	// the wrong host later.
	me, err := originOf(tok.Me)
	if err != nil || me != p.me {
		h.connectFail(w, r, p.channel, fmt.Sprintf("that token belongs to %s, not %s", tok.Me, p.me))
		return
	}
	if !hasGrantedScope(tok.Scope, feedConnectScope) {
		h.connectFail(w, r, p.channel, "the token does not carry "+feedConnectScope+" (got "+tok.Scope+")")
		return
	}

	setting, err := h.Backend.loadSetting(r.Context(), p.channel)
	if err != nil {
		h.connectFail(w, r, p.channel, "could not load the channel settings")
		return
	}
	setting.FeedToken = tok.AccessToken
	setting.FeedMe = p.me
	setting.FeedScope = tok.Scope
	setting.FeedURL = p.feed
	if err := h.Backend.saveSetting(p.channel, setting); err != nil {
		slog.ErrorContext(r.Context(), "could not save the feed token", "channel", p.channel, "error", err)
		h.connectFail(w, r, p.channel, "could not save the token")
		return
	}
	// Connecting is also how the channel starts fetching it: a token with no
	// follow would be a credential nobody uses. Following an existing feed is a
	// no-op, so this is safe when the channel already had the URL.
	if _, err := h.Backend.FollowURL(r.Context(), p.channel, p.feed); err != nil {
		slog.ErrorContext(r.Context(), "connected, but the feed was not added to the channel", "channel", p.channel, "url", p.feed, "error", err)
	}
	slog.InfoContext(r.Context(), "connected a private feed", "channel", p.channel, "host", hostOfURL(p.feed), "scope", tok.Scope)
	http.Redirect(w, r, "/settings/channel?uid="+url.QueryEscape(p.channel)+"&connected=1", http.StatusFound)
}

// handleUnfollow removes one feed from a channel. A feeds list with no way to
// remove an entry is a list that only grows, and moving a channel off a stale URL
// is the reason this reader was changed at all.
func (h *mainHandler) handleUnfollow(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	if _, ok := h.requireSession(w, r, conn); !ok {
		return
	}
	uid := r.FormValue("uid")
	feedURL := r.FormValue("url")
	if uid == "" || feedURL == "" {
		http.Redirect(w, r, "/settings", http.StatusFound)
		return
	}
	if err := h.Backend.UnfollowURL(r.Context(), uid, feedURL); err != nil {
		slog.ErrorContext(r.Context(), "could not remove a feed", "channel", uid, "url", feedURL, "error", err)
	}
	http.Redirect(w, r, "/settings/channel?uid="+url.QueryEscape(uid), http.StatusFound)
}

// handleDisconnect forgets the token. The site keeps its side of it until the
// token is revoked there, which is the honest limit of a stateless token.
func (h *mainHandler) handleDisconnect(w http.ResponseWriter, r *http.Request, conn redis.Conn) {
	if _, ok := h.requireSession(w, r, conn); !ok {
		return
	}
	uid := r.FormValue("uid")
	setting, err := h.Backend.loadSetting(r.Context(), uid)
	if err != nil {
		http.Redirect(w, r, "/settings", http.StatusFound)
		return
	}
	setting.FeedToken, setting.FeedMe, setting.FeedScope, setting.FeedURL = "", "", "", ""
	if err := h.Backend.saveSetting(uid, setting); err != nil {
		slog.ErrorContext(r.Context(), "could not clear the feed token", "channel", uid, "error", err)
	}
	http.Redirect(w, r, "/settings/channel?uid="+url.QueryEscape(uid), http.StatusFound)
}

// connectFail goes back to the channel page with the reason, which is where the
// person already is.
func (h *mainHandler) connectFail(w http.ResponseWriter, r *http.Request, uid, msg string) {
	http.Redirect(w, r, "/settings/channel?uid="+url.QueryEscape(uid)+"&connect_error="+url.QueryEscape(msg), http.StatusFound)
}

func ekRandomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(buf)
}

func hasGrantedScope(granted, want string) bool {
	for _, s := range strings.Fields(granted) {
		if s == want || s == "all" {
			return true
		}
	}
	return false
}

func hostOfURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.Host
}

// feedTokenFor returns the token to send for a feed.
func (b *memoryBackend) feedTokenFor(ctx context.Context, channel, feedURL string) string {
	setting, err := b.loadSetting(ctx, channel)
	if err != nil {
		return ""
	}
	return tokenForFeed(setting, feedURL)
}

// tokenForFeed is the whole rule: a channel's token goes to a feed on the host it
// was minted for, and nowhere else. A token for p83.nl is not a credential for
// the internet, and a feed URL that resolves somewhere else must not receive it.
func tokenForFeed(setting channelSetting, feedURL string) string {
	if setting.FeedToken == "" {
		return ""
	}
	if !strings.EqualFold(hostOfURL(feedURL), hostOfURL(setting.FeedMe)) {
		return ""
	}
	return setting.FeedToken
}
