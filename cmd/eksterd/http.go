package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"p83.nl/go/ekster/pkg/indieauth"
	"p83.nl/go/ekster/pkg/microsub"
	"p83.nl/go/ekster/pkg/util"

	"github.com/gomodule/redigo/redis"
	"willnorris.com/go/microformats"
)

//go:embed templates/*.html
var templates embed.FS

type mainHandler struct {
	Backend     *memoryBackend
	BaseURL     string
	TemplateDir string
	pool        *redis.Pool
}

type session struct {
	AuthorizationEndpoint string `redis:"authorization_endpoint"`
	Me                    string `redis:"me"`
	RedirectURI           string `redis:"redirect_uri"`
	State                 string `redis:"state"`
	LoggedIn              bool   `redis:"logged_in"`
	NextURI               string `redis:"next_uri"`
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

	CurrentChannel    microsub.Channel
	CurrentSetting    channelSetting
	ExcludedTypes     map[string]bool
	ExcludedTypeNames map[string]string

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
	App         app
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

func newMainHandler(backend *memoryBackend, baseURL, templateDir string, pool *redis.Pool) (*mainHandler, error) {
	h := &mainHandler{Backend: backend}

	h.BaseURL = baseURL

	templateDir = strings.TrimRight(templateDir, "/")
	h.TemplateDir = templateDir

	h.pool = pool

	return h, nil
}

func (h *mainHandler) templateFile(filename string) string {
	return fmt.Sprintf("%s/%s", h.TemplateDir, filename)
}

func (h *mainHandler) renderTemplate(w io.Writer, filename string, data interface{}) error {
	fsys, err := fs.Sub(templates, "templates")
	if err != nil {
		return err
	}
	t, err := template.ParseFS(fsys, "base.html", filename)
	if err != nil {
		return err
	}
	err = t.ExecuteTemplate(w, filename, data)
	if err != nil {
		return err
	}
	return nil
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
	} else if err == nil {
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

func verifyAuthCode(code, redirectURI, authEndpoint, clientID string) (bool, *authResponse, error) {
	reqData := url.Values{}
	reqData.Set("code", code)
	reqData.Set("client_id", clientID)
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

	if resp.StatusCode != 200 {
		return false, nil, fmt.Errorf("HTTP response code from authorization_endpoint (%s) %d", authEndpoint, resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "application/json") {
		var authResponse authResponse
		if err := json.NewDecoder(resp.Body).Decode(&authResponse); err != nil {
			return false, nil, fmt.Errorf("while verifying authentication response from %s: %s", authEndpoint, err)
		}
		return true, &authResponse, nil
	} else if strings.HasPrefix(contentType, "application/x-form-urlencoded") {
		var authResponse authResponse
		s, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return false, nil, fmt.Errorf("while reading response body: %s", err)
		}
		values, err := url.ParseQuery(string(s))
		if err != nil {
			return false, nil, fmt.Errorf("while reading response body: %s", err)
		}
		authResponse.Me = values.Get("me")
		return true, &authResponse, nil
	}

	return false, nil, fmt.Errorf("unknown content-type %q while verifying authorization_code", contentType)
}

func isLoggedIn(backend *memoryBackend, sess *session) bool {
	if !sess.LoggedIn {
		return false
	}

	if !backend.AuthEnabled {
		return true
	}

	if sess.Me != backend.Me {
		return false
	}

	return true
}

func performIndieauthCallback(clientID string, r *http.Request, sess *session) (bool, *authResponse, error) {
	state := r.Form.Get("state")
	if state != sess.State {
		return false, &authResponse{}, fmt.Errorf("mismatched state")
	}

	code := r.Form.Get("code")
	return verifyAuthCode(code, sess.RedirectURI, sess.AuthorizationEndpoint, clientID)
}

type app struct {
	Name    string
	IconURL string
}

func getPropString(mf *microformats.Microformat, prop string) string {
	if v, e := mf.Properties[prop]; e {
		if len(v) > 0 {
			if val, ok := v[0].(string); ok {
				return val
			}
		}
	}

	return ""
}

func getAppInfo(clientID string) (app, error) {
	var app app
	clientURL, err := url.Parse(clientID)
	if err != nil {
		return app, err
	}
	resp, err := http.Get(clientID)
	if err != nil {
		return app, err
	}
	defer resp.Body.Close()

	md := microformats.Parse(resp.Body, clientURL)

	if len(md.Items) > 0 {
		mf := md.Items[0]

		if mf.Type[0] == "h-x-app" || mf.Type[0] == "h-app" {
			app.Name = getPropString(mf, "name")
			app.IconURL = getPropString(mf, "logo")
		}
	}

	return app, nil
}

func (h *mainHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn := h.pool.Get()
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
			page.Baseurl = strings.TrimRight(h.BaseURL, "/")

			err = h.renderTemplate(w, "index.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %s\n", err)
			}
			return
		} else if r.URL.Path == "/session/callback" {
			c, err := r.Cookie("session")
			if err == http.ErrNoCookie {
				http.Redirect(w, r, "/", 302)
				return
			} else if err != nil {
				http.Error(w, "could not read cookie", 500)
				return
			}

			sessionVar := c.Value
			sess, err := loadSession(sessionVar, conn)

			verified, authResponse, err := performIndieauthCallback(h.BaseURL, r, &sess)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}
			if verified {
				sess.Me = authResponse.Me
				sess.LoggedIn = true
				saveSession(sessionVar, &sess, conn)
				log.Printf("SESSION: %#v\n", sess)
				if sess.NextURI != "" {
					http.Redirect(w, r, sess.NextURI, 302)
				} else {
					http.Redirect(w, r, "/", 302)
				}
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
					if setting, e := h.Backend.Settings[v.UID]; e {
						page.CurrentSetting = setting
					} else {
						page.CurrentSetting = channelSetting{}
					}
					if page.CurrentSetting.ChannelType == "" {
						page.CurrentSetting.ChannelType = "postgres-stream"
					}
					page.ExcludedTypeNames = map[string]string{
						"repost":   "Reposts",
						"like":     "Likes",
						"bookmark": "Bookmarks",
						"reply":    "Replies",
						"checkin":  "Checkins",
					}
					page.ExcludedTypes = make(map[string]bool)
					types := []string{"repost", "like", "bookmark", "reply", "checkin"}
					for _, v := range types {
						page.ExcludedTypes[v] = false
					}
					for _, v := range page.CurrentSetting.ExcludeType {
						page.ExcludedTypes[v] = true
					}
					break
				}
			}

			err = h.renderTemplate(w, "channel.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %s\n", err)
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

			err = h.renderTemplate(w, "logs.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %s\n", err)
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
			// page.Feeds = h.Backend.Feeds

			err = h.renderTemplate(w, "settings.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %s\n", err)
			}
			return
		} else if r.URL.Path == "/auth" {
			// check if we are logged in
			// TODO: if not logged in, make sure we get back here

			sessionVar := getSessionCookie(w, r)

			sess, err := loadSession(sessionVar, conn)

			if !isLoggedIn(h.Backend, &sess) {
				sess.NextURI = r.URL.String()
				saveSession(sessionVar, &sess, conn)
				http.Redirect(w, r, "/", 302)
				return
			}

			sess.NextURI = r.URL.String()
			saveSession(sessionVar, &sess, conn)

			query := r.URL.Query()

			// responseType := query.Get("response_type") // TODO: check response_type
			me := query.Get("me")
			clientID := query.Get("client_id")
			redirectURI := query.Get("redirect_uri")
			state := query.Get("state")
			scope := query.Get("scope")
			if scope == "" {
				scope = "create"
			}

			authReq := authRequest{
				Me:          me,
				ClientID:    clientID,
				RedirectURI: redirectURI,
				Scope:       scope,
				State:       state,
			}

			_, err = conn.Do("HMSET", redis.Args{}.Add("state:"+state).AddFlat(&authReq)...)
			if err != nil {
				log.Println(err)
				fmt.Fprintf(w, "ERROR: %q\n", err)
				return
			}

			var page authPage
			page.Session = sess
			page.Me = me
			page.ClientID = clientID
			page.RedirectURI = redirectURI
			page.Scope = scope
			page.State = state
			page.Channels, err = h.Backend.ChannelsGetList()

			app, err := getAppInfo(clientID)
			if err != nil {
				log.Println(err)
			}
			page.App = app

			err = h.renderTemplate(w, "auth.html", page)
			if err != nil {
				fmt.Fprintf(w, "ERROR: %s\n", err)
			}
			return
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

			endpoints, err := getEndpoints(me)
			if err != nil {
				http.Error(w, fmt.Sprintf("Bad Request: %s, %s", err.Error(), me), 400)
				return
			}

			state := util.RandStringBytes(16)
			redirectURI := fmt.Sprintf("%s/session/callback", h.BaseURL)

			sess, err := loadSession(sessionVar, conn)

			if err != nil {
				http.Redirect(w, r, "/", 302)
				return
			}

			sess.AuthorizationEndpoint = endpoints.AuthorizationEndpoint.String()
			sess.Me = endpoints.Me.String()
			sess.State = state
			sess.RedirectURI = redirectURI
			sess.LoggedIn = false

			err = saveSession(sessionVar, &sess, conn)
			if err != nil {
				http.Redirect(w, r, "/", 302)
				return
			}

			authenticationURL := indieauth.CreateAuthenticationURL(*endpoints.AuthorizationEndpoint, endpoints.Me.String(), h.BaseURL, redirectURI, state)
			http.Redirect(w, r, authenticationURL, 302)

			return
		} else if r.URL.Path == "/session/logout" {
			httpSessionLogout(r, w, conn)
			return
		} else if r.URL.Path == "/auth/approve" {
			// create a code
			code := util.RandStringBytes(32)
			state := r.FormValue("state")
			channel := r.FormValue("channel")

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
			grantType := r.FormValue("grant_type")
			if grantType != "authorization_code" {
				w.WriteHeader(400)
				fmt.Fprintf(w, "ERROR: grant_type is not set to %q", "authorization_code")
				return
			}
			code := r.FormValue("code")
			// clientID := r.FormValue("client_id")
			// redirectURI := r.FormValue("redirect_uri")
			// me := r.FormValue("me")

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
			if err := json.NewEncoder(w).Encode(&res); err != nil {
				log.Println(err)
				return
			}
			return
		} else if r.URL.Path == "/settings/channel" {
			// defer h.Backend.save()
			// uid := r.FormValue("uid")
			//
			// if h.Backend.Settings == nil {
			// 	h.Backend.Settings = make(map[string]channelSetting)
			// }
			//
			// excludeRegex := r.FormValue("exclude_regex")
			// includeRegex := r.FormValue("include_regex")
			// channelType := r.FormValue("type")
			//
			// setting, e := h.Backend.Settings[uid]
			// if !e {
			// 	setting = channelSetting{}
			// }
			// setting.ExcludeRegex = excludeRegex
			// setting.IncludeRegex = includeRegex
			// setting.ChannelType = channelType
			// if values, e := r.Form["exclude_type"]; e {
			// 	setting.ExcludeType = values
			// }
			// h.Backend.Settings[uid] = setting

			http.Redirect(w, r, "/settings", 302)
			return
		} else if r.URL.Path == "/refresh" {
			h.Backend.RefreshFeeds()
			http.Redirect(w, r, "/", 302)
			return
		}
	}

	http.NotFound(w, r)
}

func httpSessionLogout(r *http.Request, w http.ResponseWriter, conn redis.Conn) {
	c, err := r.Cookie("session")
	if err == http.ErrNoCookie {
		http.Redirect(w, r, "/", 302)
		return
	}
	if err == nil {
		sessionVar := c.Value
		_, _ = conn.Do("DEL", "session:"+sessionVar)
	}
	http.Redirect(w, r, "/", 302)
}

type parsedEndpoints struct {
	Me                    *url.URL
	AuthorizationEndpoint *url.URL
	TokenEndpoint         *url.URL
	MicrosubEndpoint      *url.URL
	MicropubEndpoint      *url.URL
}

func getEndpoints(me string) (parsedEndpoints, error) {
	endpoints := parsedEndpoints{}

	meURL, err := url.Parse(me)
	if err != nil {
		return endpoints, err
	}
	endpoints.Me = meURL

	eps, err := indieauth.GetEndpoints(meURL)
	if err != nil {
		return endpoints, err
	}

	authURL, err := url.Parse(eps.AuthorizationEndpoint)
	if err != nil {
		return endpoints, err
	}
	endpoints.AuthorizationEndpoint = authURL

	tokenURL, err := url.Parse(eps.TokenEndpoint)
	if err != nil {
		return endpoints, err
	}
	endpoints.TokenEndpoint = tokenURL

	microsubEndpoint, err := url.Parse(eps.MicrosubEndpoint)
	if err != nil {
		return endpoints, err
	}
	endpoints.MicrosubEndpoint = microsubEndpoint

	micropubEndpoint, err := url.Parse(eps.MicropubEndpoint)
	if err != nil {
		return endpoints, err
	}
	endpoints.MicropubEndpoint = micropubEndpoint

	return endpoints, err
}
