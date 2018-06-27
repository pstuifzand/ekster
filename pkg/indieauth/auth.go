package indieauth

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"

	"willnorris.com/go/microformats"
)

type Endpoints struct {
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	MicropubEndpoint      string `json:"micropub_endpoint"`
	MicrosubEndpoint      string `json:"microsub_endpoint"`
}

type TokenResponse struct {
	Me          string `json:"me"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       string `json:"scope"`
}

func GetEndpoints(me *url.URL) (Endpoints, error) {
	var endpoints Endpoints

	baseURL := me

	res, err := http.Get(me.String())
	if err != nil {
		return endpoints, err
	}
	defer res.Body.Close()

	data := microformats.Parse(res.Body, baseURL)

	if auth, e := data.Rels["authorization_endpoint"]; e {
		endpoints.AuthorizationEndpoint = auth[0]
	}
	if token, e := data.Rels["token_endpoint"]; e {
		endpoints.TokenEndpoint = token[0]
	}
	if micropub, e := data.Rels["micropub"]; e {
		endpoints.MicropubEndpoint = micropub[0]
	}
	if microsub, e := data.Rels["microsub"]; e {
		endpoints.MicrosubEndpoint = microsub[0]
	}

	return endpoints, nil
}

func Authorize(me *url.URL, endpoints Endpoints, clientID, scope string) (TokenResponse, error) {
	var tokenResponse TokenResponse

	authURL, err := url.Parse(endpoints.AuthorizationEndpoint)
	if err != nil {
		return tokenResponse, err
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return tokenResponse, err
	}

	local := ln.Addr().String()
	redirectURI := fmt.Sprintf("http://%s/", local)
	state := "12345344"

	q := authURL.Query()
	q.Add("response_type", "code")
	q.Add("me", me.String())
	q.Add("client_id", clientID)
	q.Add("redirect_uri", redirectURI)
	q.Add("state", state)
	q.Add("scope", scope)
	authURL.RawQuery = q.Encode()

	log.Printf("Browse to %s\n", authURL.String())

	shutdown := make(chan struct{}, 1)

	code := ""

	handler := func(w http.ResponseWriter, r *http.Request) {
		code = r.URL.Query().Get("code")
		responseState := r.URL.Query().Get("state")
		if state != responseState {
			log.Println("Wrong state response")
		}
		close(shutdown)
	}

	var srv http.Server
	srv.Handler = http.HandlerFunc(handler)

	idleConnsClosed := make(chan struct{})

	go func() {
		<-shutdown

		// We received an interrupt signal, shut down.
		if err := srv.Shutdown(context.Background()); err != nil {
			// Error from closing listeners, or context timeout:
			log.Printf("HTTP server Shutdown: %v", err)
		}
		close(idleConnsClosed)
	}()

	if err := srv.Serve(ln); err != http.ErrServerClosed {
		// Error starting or closing listener:
		log.Printf("HTTP server ListenAndServe: %v", err)
	}

	<-idleConnsClosed

	reqValues := url.Values{}
	reqValues.Add("grant_type", "authorization_code")
	reqValues.Add("code", code)
	reqValues.Add("redirect_uri", redirectURI)
	reqValues.Add("client_id", clientID)
	reqValues.Add("me", me.String())

	res, err := http.PostForm(endpoints.TokenEndpoint, reqValues)
	if err != nil {
		return tokenResponse, err
	}

	defer res.Body.Close()

	dec := json.NewDecoder(res.Body)
	err = dec.Decode(&tokenResponse)
	if err != nil {
		return tokenResponse, err
	}

	return tokenResponse, nil
}
