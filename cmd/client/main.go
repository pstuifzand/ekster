package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/pstuifzand/microsub-server/pkg/client"
	"github.com/pstuifzand/microsub-server/pkg/indieauth"
)

func loadAuth(c *client.Client, filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	var token indieauth.TokenResponse
	dec := json.NewDecoder(f)
	err = dec.Decode(&token)
	if err != nil {
		return err
	}
	c.Token = token.AccessToken

	u, err := url.Parse(token.Me)
	if err != nil {
		return err
	}

	c.Me = u
	return nil
}

func loadEndpoints(c *client.Client, me *url.URL, filename string) error {
	var endpoints indieauth.Endpoints

	f, err := os.Open(filename)
	if err != nil {
		f, err = os.Create(filename)
		if err != nil {
			return err
		}
		defer f.Close()

		endpoints, err = indieauth.GetEndpoints(me)
		if err != nil {
			return err
		}

		enc := json.NewEncoder(f)
		err = enc.Encode(&endpoints)
		if err != nil {
			return err
		}
	} else {
		defer f.Close()

		dec := json.NewDecoder(f)
		err = dec.Decode(&endpoints)
		if err != nil {
			return err
		}
	}

	if endpoints.MicrosubEndpoint == "" {
		return fmt.Errorf("Microsub Endpoint is missing")
	}

	u, err := url.Parse(endpoints.MicrosubEndpoint)
	if err != nil {
		return err
	}

	c.MicrosubEndpoint = u
	return nil
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "connect" {
		err := os.MkdirAll("/home/peter/.config/microsub/", os.FileMode(0770))
		if err != nil {
			log.Fatal(err)
		}

		f, err := os.Create("/home/peter/.config/microsub/client.json")
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()

		me, err := url.Parse(os.Args[2])
		if err != nil {
			log.Fatal(err)
		}

		endpoints, err := indieauth.GetEndpoints(me)
		if err != nil {
			log.Fatal(err)
		}

		token, err := indieauth.Authorize(me, endpoints)
		if err != nil {
			log.Fatal(err)
		}

		enc := json.NewEncoder(f)
		err = enc.Encode(token)
		if err != nil {
			log.Fatal(err)
		}

		log.Println("Authorization successful")

		return
	}

	var c client.Client
	err := loadAuth(&c, "/home/peter/.config/microsub/client.json")
	if err != nil {
		log.Fatal(err)
	}

	err = loadEndpoints(&c, c.Me, "/home/peter/.config/microsub/endpoints.json")
	if err != nil {
		log.Fatal(err)
	}

	if len(os.Args) == 2 && os.Args[1] == "channels" {
		channels := c.ChannelsGetList()

		for _, ch := range channels {
			fmt.Println(ch.UID, " ", ch.Name)
		}
	}
}
