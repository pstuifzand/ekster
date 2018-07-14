package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/alecthomas/template"
	"github.com/garyburd/redigo/redis"
	"github.com/pstuifzand/ekster/pkg/indieauth"
	"github.com/pstuifzand/ekster/pkg/microsub"
	"github.com/pstuifzand/ekster/pkg/util"
)

type mainHandler struct {
	Backend   *memoryBackend
	Templates *template.Template
}

type session struct {
	AuthorizationEndpoint string `redis:"authorization_endpoint"`
	Me                    string `redis:"me"`
	RedirectURI           string `redis:"redirect_uri"`
	State                 string `redis:"state"`
	LoggedIn              bool   `redis:"logged_in"`
}

type authResponse struct {
	Me string `json:"me"`
}

type authTokenResponse struct {
	Me          string `json:"me"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

type indexPage struct {
	Session session
	Baseurl string
}
type settingsPage struct {
	Session session

	CurrentChannel microsub.Channel

	Channels []microsub.Channel
	Feeds    []microsub.Feed
}
type logsPage struct {
	Session session
}

type authPage struct {
	Session     session
	Me          string
	ClientID    string
	Scope       string
	RedirectURI string
	State       string
	Channels    []microsub.Channel
}

type authRequest struct {
	Me          string `redis:"me"`
	ClientID    string `redis:"client_id"`
	Scope       string `redis:"scope"`
	RedirectURI string `redis:"redirect_uri"`
	State       string `redis:"state"`
	Code        string `redis:"code"`
	Channel     string `redis:"channel"`
	AccessToken string `redis:"access_token"`
}

func newMainHandler(backend *memoryBackend) (*mainHandler, error) {
	h := &mainHandler{Backend: backend}

	templateDir := os.Getenv("EKSTER_TEMPLATES")
	if templateDir == "" {
		return nil, fmt.Errorf("Missing env var EKSTER_TEMPLATES")
	}

	templateDir = strings.TrimRight(templateDir, "/")

	templates, err := template.ParseGlob(fmt.Sprintf("%s/*.html", templateDir))
	if err != nil {
		return nil, err
	}

	h.Templates = templates
	return h, nil
}

func getSessionCookie(w http.ResponseWriter, r *http.Request) string {
	c, err := r.Cookie("session")
	sessionVar := util.RandStringBytes(16)

	if err == http.ErrNoCookie {
		newCookie := &http.Cookie{
			Name:    "session",
			Value:   sessionVar,
			Expires: time.Now().Add(24 * time.Hour),
		}

		http.SetCookie(w, newCookie)
	} else {
		sessionVar = c.Value
	}

	return sessionVar
}

func loadSession(sessionVar string, conn redis.Conn) (session, error) {
	var sess session
	sessionKey := "session:" + sessionVar
	data, err := redis.Values(conn.Do("HGETALL", sessionKey))
	if err != nil {
		return sess, err
	}
	err = redis.ScanStruct(data, &sess)
	if err != nil {
		return sess, err
	}
	return sess, nil
}

func saveSession(sessionVar string, sess *session, conn redis.Conn) error {
	_, err := conn.Do("HMSET", redis.Args{}.Add("session:"+sessionVar).AddFlat(sess)...)
	return err
}

func verifyAuthCode(code, redirectURI, authEndpoint string) (bool, *authResponse, error) {
	reqData := url.Values{}
	reqData.Set("code", code)
	reqData.Set("client_id", ClientID)
	reqData.Set("redirect_uri", redirectURI)

	req, err := http.NewRequest(http.MethodPost, authEndpoint, strings.NewReader(reqData.Encode()))
	if err != nil {
		return false, nil, err
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		input := io.TeeReader(resp.Body, os.Stderr)
		dec := json.NewDecoder(input)
		var authResponse authResponse
		err = dec.Decode(&authResponse)
		if err != nil {
			return false, nil, err
		}

		return true, &authResponse, nil
	}

	return false, nil, fmt.Errorf("HTTP response code from authorization_endpoint (%s) %d", authEndpoint, resp.StatusCode)
}

func isLoggedIn(backend *memoryBackend, sess *session) bool {
	if !sess.LoggedIn {
		return false
	}

	if sess.Me != backend.Me {
		return false
	}

	return true
}

func (h *mainHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn := pool.Get()
	defer conn.Close()

	err := r.ParseForm()
	if err != nil {
		log.Println(err)
		http.Error(w, fmt.Sprintf("Bad Request: %s", err.Error()), 400)
		return
	}

	if r.Method == http.MethodGet {
		if r.URL.Path == "/" {
			sessionVar := getSessionCookie(w, r)
			sess, err := loadSession(sessionVar, conn)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			var page indexPage
			page.Session = sess
			page.Baseurl = strings.TrimRight(os.Getenv("EKSTER_BASEURL"), "/")

			err = h.Templates.ExecuteTemplate(w, "index.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			return
		} else if r.URL.Path == "/session/callback" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}

			sessionVar := c.Value
			sess, err := loadSession(sessionVar, conn)

			state := r.Form.Get("state")
			if state != sess.State {
				w.WriteHeader(http.StatusBadRequest)
				fmt.Fprintf(w, "ERROR: Mismatched state\n")
				return
			}
			code := r.Form.Get("code")

			verified, authResponse, err := verifyAuthCode(code, sess.RedirectURI, sess.AuthorizationEndpoint)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			if verified {
				sess.Me = authResponse.Me
				sess.LoggedIn = true
				saveSession(sessionVar, &sess, conn)
				http.Redirect(w, r, "/", 302)
				return
			}
			return
		} else if r.URL.Path == "/settings/channel" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}
			sessionVar := c.Value
			sess, err := loadSession(sessionVar, conn)

			if !isLoggedIn(h.Backend, &sess) {
				w.WriteHeader(401)
				fmt.Fprintf(w, "Unauthorized")
				return
			}

			var page settingsPage
			page.Session = sess
			currentChannel := r.URL.Query().Get("uid")
			page.Channels, err = h.Backend.ChannelsGetList()
			page.Feeds, err = h.Backend.FollowGetList(currentChannel)

			for _, v := range page.Channels {
				if v.UID == currentChannel {
					page.CurrentChannel = v
					break
				}
			}

			err = h.Templates.ExecuteTemplate(w, "channel.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			return
		} else if r.URL.Path == "/logs" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}
			sessionVar := c.Value
			sess, err := loadSession(sessionVar, conn)

			if !isLoggedIn(h.Backend, &sess) {
				w.WriteHeader(401)
				fmt.Fprintf(w, "Unauthorized")
				return
			}

			var page logsPage
			page.Session = sess

			err = h.Templates.ExecuteTemplate(w, "logs.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			return
		} else if r.URL.Path == "/settings" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}
			sessionVar := c.Value
			sess, err := loadSession(sessionVar, conn)

			if !isLoggedIn(h.Backend, &sess) {
				w.WriteHeader(401)
				fmt.Fprintf(w, "Unauthorized")
				return
			}

			var page settingsPage
			page.Session = sess
			page.Channels, err = h.Backend.ChannelsGetList()
			//page.Feeds = h.Backend.Feeds

			err = h.Templates.ExecuteTemplate(w, "settings.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			return
		} else if r.URL.Path == "/auth" {
			// check if we are logged in
			// TODO: if not logged in, make sure we get back here
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}
			sessionVar := c.Value
			sess, err := loadSession(sessionVar, conn)

			if !isLoggedIn(h.Backend, &sess) {
				http.Redirect(w, r, "/", 302)
				return
			}

			query := r.URL.Query()

			//responseType := query.Get("response_type")
			me := query.Get("me")
			clientID := query.Get("client_id")
			redirectURI := query.Get("redirect_uri")
			state := query.Get("state")
			scope := query.Get("scope")
			if scope == "" {
				scope = "create"
			}

			auth := authRequest{
				Me:          me,
				ClientID:    clientID,
				RedirectURI: redirectURI,
				Scope:       scope,
				State:       state,
			}

			_, err = conn.Do("HMSET", redis.Args{}.Add("state:"+state).AddFlat(&auth)...)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			// Save this ^^ in Redis

			var page authPage
			page.Session = sess
			page.Me = me
			page.ClientID = clientID
			page.RedirectURI = redirectURI
			page.Scope = scope
			page.State = state
			page.Channels, err = h.Backend.ChannelsGetList()

			err = h.Templates.ExecuteTemplate(w, "auth.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			return

		} else if r.URL.Path == "/auth/token" {
		}

	} else if r.Method == http.MethodPost {
		if r.URL.Path == "/session" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}

			sessionVar := c.Value

			// redirect to endpoint
			me := r.Form.Get("url")
			log.Println(me)
			meURL, err := url.Parse(me)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s, %s", err.Error(), me), 400)
				return
			}
			endpoints, err := indieauth.GetEndpoints(meURL)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s %s", err.Error(), me), 400)
				return
			}
			log.Println(endpoints)

			authURL, err := url.Parse(endpoints.AuthorizationEndpoint)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s %s", err.Error(), me), 400)
				return
			}
			log.Println(authURL)

			state := util.RandStringBytes(16)
			redirectURI := fmt.Sprintf("%s/session/callback", os.Getenv("EKSTER_BASEURL"))

			sess := session{
				AuthorizationEndpoint: endpoints.AuthorizationEndpoint,
				Me:          meURL.String(),
				State:       state,
				RedirectURI: redirectURI,
				LoggedIn:    false,
			}
			saveSession(sessionVar, &sess, conn)

			q := authURL.Query()
			q.Add("response_type", "id")
			q.Add("me", meURL.String())
			q.Add("client_id", ClientID)
			q.Add("redirect_uri", redirectURI)
			q.Add("state", state)
			authURL.RawQuery = q.Encode()

			http.Redirect(w, r, authURL.String(), 302)
			return
		} else if r.URL.Path == "/session/logout" {
			c, err := r.Cookie("session")

			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			}

			sessionVar := c.Value
			conn.Do("DEL", "session:"+sessionVar)
			http.Redirect(w, r, "/", 302)
			return
		} else if r.URL.Path == "/auth/approve" {
			// create a code
			code := util.RandStringBytes(32)
			state := r.FormValue("state")
			channel := r.FormValue("channel")
			log.Println(code, state, channel)

			values, err := redis.Values(conn.Do("HGETALL", "state:"+state))
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			var auth authRequest
			err = redis.ScanStruct(values, &auth)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			auth.Code = code
			auth.Channel = channel
			_, err = conn.Do("HMSET", redis.Args{}.Add("code:"+code).AddFlat(&auth)...)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			_, err = conn.Do("EXPIRE", "code:"+code, 5*60)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}

			redirectURI, err := url.Parse(auth.RedirectURI)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			log.Println(redirectURI)
			q := redirectURI.Query()
			q.Add("code", code)
			q.Add("state", auth.State)
			redirectURI.RawQuery = q.Encode()

			log.Println(redirectURI)
			http.Redirect(w, r, redirectURI.String(), 302)
			return
		} else if r.URL.Path == "/auth/token" {
			//grantType := r.FormValue("grant_type")
			code := r.FormValue("code")
			//clientID := r.FormValue("client_id")
			//redirectURI := r.FormValue("redirect_uri")
			//me := r.FormValue("me")

			values, err := redis.Values(conn.Do("HGETALL", "code:"+code))
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			var auth authRequest
			err = redis.ScanStruct(values, &auth)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			token := util.RandStringBytes(32)
			_, err = conn.Do("HMSET", redis.Args{}.Add("token:"+token).AddFlat(&auth)...)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}

			res := authTokenResponse{
				Me:          auth.Me,
				AccessToken: token,
				TokenType:   "Bearer",
				Scope:       auth.Scope,
			}

			w.Header().Add("Content-Type", "application/json")
			enc := json.NewEncoder(w)
			err = enc.Encode(&res)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q", err)
				return
			}
			return
		}
	}

	http.NotFound(w, r)
}
