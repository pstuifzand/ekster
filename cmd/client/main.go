package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/pstuifzand/microsub-server/microsub"
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

	performCommands(&c, os.Args)
}

func performCommands(sub microsub.Microsub, commands []string) {

	if len(commands) == 2 && commands[1] == "channels" {
		channels := sub.ChannelsGetList()
		for _, ch := range channels {
			fmt.Println(ch.UID, " ", ch.Name)
		}
	}

	if len(commands) >= 3 && commands[1] == "timeline" {
		channel := commands[2]

		var timeline microsub.Timeline

		if len(commands) == 5 && commands[3] == "-after" {
			timeline = sub.TimelineGet("", commands[4], channel)
		} else if len(commands) == 5 && commands[3] == "-before" {
			timeline = sub.TimelineGet(commands[4], "", channel)
		} else {
			timeline = sub.TimelineGet("", "", channel)
		}

		for _, item := range timeline.Items {
			showItem(&item)
		}

		fmt.Printf("Before: %s, After: %s\n", timeline.Paging.Before, timeline.Paging.After)
	}

	if len(commands) == 3 && commands[1] == "search" {
		query := commands[2]
		feeds := sub.Search(query)

		for _, feed := range feeds {
			fmt.Println(feed.Name, " ", feed.URL)
		}
	}

	if len(commands) == 3 && commands[1] == "preview" {
		url := commands[2]
		timeline := sub.PreviewURL(url)

		for _, item := range timeline.Items {
			showItem(&item)
		}
	}

	if len(commands) == 3 && commands[1] == "follow" {
		uid := commands[2]
		feeds := sub.FollowGetList(uid)
		for _, feed := range feeds {
			fmt.Println(feed.Name, " ", feed.URL)
		}
	}

	if len(commands) == 4 && commands[1] == "follow" {
		uid := commands[2]
		url := commands[3]
		sub.FollowURL(uid, url)
	}

	if len(commands) == 4 && commands[1] == "unfollow" {
		uid := commands[2]
		url := commands[3]
		sub.UnfollowURL(uid, url)
	}
}

func showItem(item *microsub.Item) {
	fmt.Printf("------- %s\n", item.Published)
	if item.Name == "" && item.Content != nil {
		fmt.Println(item.Content.HTML)
	} else {
		fmt.Println(item.Name)
	}
	fmt.Println(item.URL)
}
