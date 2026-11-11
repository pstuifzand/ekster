package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/indieauth"
	"github.com/pstuifzand/ekster/pkg/util"
)

//go:embed reader/dist
var readerFiles embed.FS

type readerServer struct {
	mu               sync.RWMutex
	client           client.Client
	micropubEndpoint *url.URL
	configDir        string
	callbackURL      string
	pending          map[string]pendingAuth
}

type pendingAuth struct {
	me        *url.URL
	endpoints indieauth.Endpoints
	state     string
}

func runReader() error {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "microsub")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	defer ln.Close()
	baseURL := "http://" + ln.Addr().String()
	s := &readerServer{configDir: configDir, callbackURL: baseURL + "/_ek/callback", pending: make(map[string]pendingAuth)}
	_ = s.loadSavedClient()
	root, err := fs.Sub(readerFiles, "reader/dist")
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/_ek/api/connection", s.connection)
	mux.HandleFunc("/_ek/api/microsub", s.microsub)
	mux.HandleFunc("/_ek/api/micropub", s.micropub)
	mux.HandleFunc("/_ek/callback", s.callback)
	files := http.FileServer(http.FS(root))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cleanPath := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
		if cleanPath != "." {
			if _, err := fs.Stat(root, cleanPath); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		index, err := fs.ReadFile(root, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	}))
	server := &http.Server{Handler: localOnly(mux, ln.Addr().String()), ReadHeaderTimeout: 5 * time.Second}
	fmt.Printf("Ek Reader: %s\n", baseURL)
	if err := exec.Command("xdg-open", baseURL).Start(); err != nil {
		log.Printf("Open %s in your browser", baseURL)
	}
	return server.Serve(ln)
}

func localOnly(next http.Handler, expectedHost string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || !net.ParseIP(host).IsLoopback() || r.Host != expectedHost {
			http.Error(w, "loopback clients only", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+expectedHost {
			http.Error(w, "cross-origin request denied", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *readerServer) loadSavedClient() error {
	var token indieauth.TokenResponse
	f, err := os.Open(filepath.Join(s.configDir, "client.json"))
	if err != nil {
		return err
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&token); err != nil {
		return err
	}
	if token.AccessToken == "" || token.Me == "" {
		return fmt.Errorf("saved IndieAuth connection is incomplete")
	}
	me, err := url.Parse(token.Me)
	if err != nil {
		return err
	}
	var endpoints indieauth.Endpoints
	ef, err := os.Open(filepath.Join(s.configDir, "endpoints.json"))
	if err == nil {
		defer ef.Close()
		if err := json.NewDecoder(ef).Decode(&endpoints); err != nil {
			return err
		}
	} else if os.IsNotExist(err) {
		endpoints, err = indieauth.GetEndpoints(me)
		if err != nil {
			return err
		}
		if endpoints.MicrosubEndpoint == "" {
			return fmt.Errorf("saved account has no Microsub endpoint")
		}
		if err = writeJSONFile(filepath.Join(s.configDir, "endpoints.json"), endpoints); err != nil {
			return err
		}
	} else {
		return err
	}
	if endpoints.MicrosubEndpoint == "" {
		return fmt.Errorf("saved account has no Microsub endpoint")
	}
	endpoint, err := url.Parse(endpoints.MicrosubEndpoint)
	if err != nil {
		return err
	}
	s.client = client.Client{Me: me, Token: token.AccessToken, MicrosubEndpoint: endpoint}
	if endpoints.MicropubEndpoint != "" {
		s.micropubEndpoint, _ = url.Parse(endpoints.MicropubEndpoint)
	}
	return nil
}

func (s *readerServer) connection(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		s.mu.RLock()
		c := s.client
		s.mu.RUnlock()
		if c.Token == "" || c.Me == nil {
			writeJSON(w, http.StatusOK, map[string]any{"connected": false})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"connected": true, "me": c.Me.String(), "microsubEndpoint": "/_ek/api/microsub", "micropubEndpoint": "/_ek/api/micropub"})
	case http.MethodPost:
		var input struct {
			Me string `json:"me"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		me, err := url.ParseRequestURI(input.Me)
		if err != nil || (me.Scheme != "https" && me.Scheme != "http") || me.Host == "" {
			http.Error(w, "me must be an http(s) URL", http.StatusBadRequest)
			return
		}
		endpoints, err := indieauth.GetEndpoints(me)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if endpoints.MicrosubEndpoint == "" {
			http.Error(w, "website has no Microsub endpoint", http.StatusBadRequest)
			return
		}
		authEndpoint, err := url.Parse(endpoints.AuthorizationEndpoint)
		if err != nil || authEndpoint.Host == "" {
			http.Error(w, "missing authorization endpoint", http.StatusBadRequest)
			return
		}
		state := util.RandStringBytes(24)
		s.mu.Lock()
		s.pending[state] = pendingAuth{me: me, endpoints: endpoints, state: state}
		s.mu.Unlock()
		authURL := indieauth.CreateAuthorizationURL(*authEndpoint, me.String(), s.callbackURL, s.callbackURL, state, "create post read follow channels timeline")
		writeJSON(w, http.StatusOK, map[string]string{"authorization_url": authURL})
	case http.MethodDelete:
		s.mu.Lock()
		s.client = client.Client{}
		s.micropubEndpoint = nil
		s.mu.Unlock()
		_ = os.Remove(filepath.Join(s.configDir, "client.json"))
		_ = os.Remove(filepath.Join(s.configDir, "endpoints.json"))
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *readerServer) callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	s.mu.Lock()
	pending, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()
	if !ok || state == "" || r.URL.Query().Get("error") != "" {
		http.Error(w, "IndieAuth authorization failed", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing authorization code", http.StatusBadRequest)
		return
	}
	values := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {s.callbackURL}, "client_id": {s.callbackURL}, "me": {pending.me.String()}}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, pending.endpoints.TokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		http.Error(w, "could not create token request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "token endpoint unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "token endpoint rejected authorization", http.StatusBadGateway)
		return
	}
	var token indieauth.TokenResponse
	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token)
	} else {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if readErr != nil {
			err = readErr
		} else {
			var form url.Values
			form, err = url.ParseQuery(string(body))
			token = indieauth.TokenResponse{Me: form.Get("me"), AccessToken: form.Get("access_token"), TokenType: form.Get("token_type"), Scope: form.Get("scope")}
			if token.AccessToken == "" {
				token.AccessToken = form.Get("token")
			}
		}
	}
	if err != nil || token.AccessToken == "" || (token.Me != "" && token.Me != pending.me.String()) {
		http.Error(w, "invalid token response", http.StatusBadGateway)
		return
	}
	token.Me = pending.me.String()
	if err = os.MkdirAll(s.configDir, 0700); err != nil {
		http.Error(w, "cannot save credentials", http.StatusInternalServerError)
		return
	}
	if err = writeJSONFile(filepath.Join(s.configDir, "client.json"), token); err != nil {
		http.Error(w, "cannot save credentials", http.StatusInternalServerError)
		return
	}
	if err = writeJSONFile(filepath.Join(s.configDir, "endpoints.json"), pending.endpoints); err != nil {
		http.Error(w, "cannot save endpoints", http.StatusInternalServerError)
		return
	}
	endpoint, err := url.Parse(pending.endpoints.MicrosubEndpoint)
	if err != nil {
		http.Error(w, "invalid Microsub endpoint", http.StatusBadGateway)
		return
	}
	s.mu.Lock()
	s.client = client.Client{Me: pending.me, Token: token.AccessToken, MicrosubEndpoint: endpoint}
	s.micropubEndpoint = nil
	if pending.endpoints.MicropubEndpoint != "" {
		s.micropubEndpoint, _ = url.Parse(pending.endpoints.MicropubEndpoint)
	}
	s.mu.Unlock()
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *readerServer) microsub(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, false) }
func (s *readerServer) micropub(w http.ResponseWriter, r *http.Request) { s.proxy(w, r, true) }

func (s *readerServer) proxy(w http.ResponseWriter, r *http.Request, micropub bool) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.RLock()
	upstream := s.client.MicrosubEndpoint
	if micropub {
		upstream = s.micropubEndpoint
	}
	token := s.client.Token
	s.mu.RUnlock()
	if upstream == nil || token == "" {
		http.Error(w, "Ek is not connected", http.StatusUnauthorized)
		return
	}
	target := *upstream
	target.RawQuery = r.URL.RawQuery
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if ct := r.Header.Get("Content-Type"); ct != "" {
		req.Header.Set("Content-Type", ct)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		http.Error(w, "Microsub server unavailable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, name := range []string{"Content-Type", "Cache-Control", "Last-Modified", "ETag"} {
		if value := resp.Header.Get(name); value != "" {
			w.Header().Set(name, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeJSONFile(path string, value any) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		return err
	}
	return json.NewEncoder(f).Encode(value)
}
