package indieauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"

	"linkheader"

	"p83.nl/go/ekster/pkg/util"
	"willnorris.com/go/microformats"
)

// Endpoints contain the endpoints used by Ekster
type Endpoints struct {
	Me                    string `json:"me"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	MicropubEndpoint      string `json:"micropub_endpoint"`
	MicrosubEndpoint      string `json:"microsub_endpoint"`
}

// TokenResponse contains the response from a token request to an IndieAuth server
type TokenResponse struct {
	Me               string `json:"me"`
	AccessToken      string `json:"access_token"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// GetEndpoints returns the endpoints for the me url
func GetEndpoints(me *url.URL) (Endpoints, error) {
	var endpoints Endpoints
	endpoints.Me = me.String()

	baseURL := me

	res, err := http.Get(me.String())
	if err != nil {
		return endpoints, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return endpoints, fmt.Errorf("error from endpoint while reading body %d", res.StatusCode)
		}
		return endpoints, fmt.Errorf("error from endpoint %d: %s", res.StatusCode, body)
	}

	var links linkheader.Links

	if headers, e := res.Header["Link"]; e {
		links = linkheader.ParseMultiple(headers)
		for _, link := range links {
			if link.Rel == "authorization_endpoint" {
				endpoints.AuthorizationEndpoint = link.URL
			} else if link.Rel == "token_endpoint" {
				endpoints.TokenEndpoint = link.URL
			} else if link.Rel == "micropub" {
				endpoints.MicropubEndpoint = link.URL
			} else if link.Rel == "microsub" {
				endpoints.MicrosubEndpoint = link.URL
			} else {
				log.Printf("Skipping unsupported rels in Link header: %s %s\n", link.Rel, link.URL)
			}
		}
	}

	data := microformats.Parse(res.Body, baseURL)

	if auth, e := data.Rels["authorization_endpoint"]; e && endpoints.AuthorizationEndpoint == "" {
		endpoints.AuthorizationEndpoint = auth[0]
	}
	if token, e := data.Rels["token_endpoint"]; e && endpoints.TokenEndpoint == "" {
		endpoints.TokenEndpoint = token[0]
	}
	if micropub, e := data.Rels["micropub"]; e && endpoints.MicropubEndpoint == "" {
		endpoints.MicropubEndpoint = micropub[0]
	}
	if microsub, e := data.Rels["microsub"]; e && endpoints.MicrosubEndpoint == "" {
		endpoints.MicrosubEndpoint = microsub[0]
	}

	return endpoints, nil
}

// Authorize allows you to get the token from Indieauth through the command line
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
	state := util.RandStringBytes(16)

	authorizationURL := CreateAuthorizationURL(*authURL, me.String(), clientID, redirectURI, state, scope)

	log.Printf("Browse to %s\n", authorizationURL)

	shutdown := make(chan struct{}, 1)

	code := ""

	handler := func(w http.ResponseWriter, r *http.Request) {
		code = r.URL.Query().Get("code")
		responseState := r.URL.Query().Get("state")
		if state != responseState {
			log.Println("Wrong state response")
		}
		fmt.Fprintln(w, `<div style="width:100%;height:100%;display: flex; align-items: center; justify-content: center;">You can close this window, proceed on the command line</div>`)
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

	req, err := http.NewRequest(http.MethodPost, endpoints.TokenEndpoint, strings.NewReader(reqValues.Encode()))
	if err != nil {
		return tokenResponse, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return tokenResponse, err
	}
	defer res.Body.Close()

	if res.StatusCode != 200 {
		return TokenResponse{}, fmt.Errorf("status code %d, instead of 200", res.StatusCode)
	}

	if strings.HasPrefix(res.Header.Get("content-type"), "application/json") {
		dec := json.NewDecoder(res.Body)
		err = dec.Decode(&tokenResponse)
		if err != nil {
			return tokenResponse, fmt.Errorf("error while parsing response body with content-type %s as json: %s", res.Header.Get("content-type"), err)
		}
		if tokenResponse.Me == "" && tokenResponse.Error != "" {
			return tokenResponse, fmt.Errorf("received error from endpoint: %s, %s", tokenResponse.Error, tokenResponse.ErrorDescription)
		}
	} else if strings.HasPrefix(res.Header.Get("content-type"), "application/x-www-form-urlencoded") {
		body, err := ioutil.ReadAll(res.Body)
		if err != nil {
			return tokenResponse, fmt.Errorf("error while reading response body with content-type %s: %s", res.Header.Get("content-type"), err)
		}

		values, err := url.ParseQuery(string(body))
		if err != nil {
			return tokenResponse, fmt.Errorf("error while parsing response body with content-type %s as application/x-www-form-urlencoded: %s; body was: %q", res.Header.Get("content-type"), err, body)
		}

		if values.Get("me") == "" {
			if errTxt := values.Get("error"); errTxt != "" {
				return tokenResponse, fmt.Errorf("received error from endpoint: %s, %s", errTxt, values.Get("error_description"))
			}
		}
		tokenResponse.Me = values.Get("me")
		tokenResponse.AccessToken = values.Get("token")
		tokenResponse.TokenType = values.Get("token_type")
		tokenResponse.Scope = values.Get("scope")
	}

	return tokenResponse, nil
}

// CreateAuthenticationURL builds the url for authentication
func CreateAuthenticationURL(authURL url.URL, meURL, clientID, redirectURI, state string) string {
	q := authURL.Query()

	q.Add("response_type", "id")
	q.Add("me", meURL)
	q.Add("client_id", clientID)
	q.Add("redirect_uri", redirectURI)
	q.Add("state", state)

	authURL.RawQuery = q.Encode()

	return authURL.String()
}

// CreateAuthorizationURL builds the url for authorization
func CreateAuthorizationURL(authURL url.URL, meURL, clientID, redirectURI, state, scope string) string {
	q := authURL.Query()
	q.Add("response_type", "code")
	q.Add("me", meURL)
	q.Add("client_id", clientID)
	q.Add("redirect_uri", redirectURI)
	q.Add("state", state)
	q.Add("scope", scope)
	authURL.RawQuery = q.Encode()
	return authURL.String()
}
