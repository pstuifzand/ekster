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
	return nil
}

func loadEndpoints(c *client.Client, me, filename string) error {
	endpoints, err := indieauth.GetEndpoints(me)
	if err != nil {
		return err
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

		me := os.Args[2]
		endpoints, err := indieauth.GetEndpoints(me)

		token, err := indieauth.Authorize(me, endpoints)
		if err != nil {
			log.Fatal(err)
		}

		enc := json.NewEncoder(f)
		enc.Encode(token)

		log.Println("Authorization successful")

		return
	} else if len(os.Args) == 3 && os.Args[1] == "channels" {
		me := os.Args[2]

		var c client.Client

		err := loadAuth(&c, "/home/peter/.config/microsub/client.json")
		if err != nil {
			log.Fatal(err)
		}

		err = loadEndpoints(&c, me, "/home/peter/.config/microsub/endpoints.json")
		if err != nil {
			log.Fatal(err)
		}

		channels := c.ChannelsGetList()

		for _, ch := range channels {
			fmt.Println(ch.UID, " ", ch.Name)
		}
	}
}
