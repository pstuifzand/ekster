package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/indieauth"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

func init() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
}

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
	configDir := fmt.Sprintf("%s/.config/microsub", os.Getenv("HOME"))

	if len(os.Args) == 3 && os.Args[1] == "connect" {
		err := os.MkdirAll(configDir, os.FileMode(0770))
		if err != nil {
			log.Fatal(err)
		}

		f, err := os.Create(fmt.Sprintf("%s/client.json", configDir))
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

		clientID := "https://p83.nl/microsub-client"
		scope := "read follow mute block channels"

		token, err := indieauth.Authorize(me, endpoints, clientID, scope)
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
	err := loadAuth(&c, fmt.Sprintf("%s/client.json", configDir))
	if err != nil {
		log.Fatal(err)
	}

	err = loadEndpoints(&c, c.Me, fmt.Sprintf("%s/endpoints.json", configDir))
	if err != nil {
		log.Fatal(err)
	}

	performCommands(&c, os.Args[1:])
}

func performCommands(sub microsub.Microsub, commands []string) {
	if len(commands) == 0 {
		fmt.Printf(`%s <command> options...

Commands:

  connect URL                  login to Indieauth url

  channels                     list channels
  channels NAME                create channel with NAME
  channels UID NAME            update channel UID with NAME
  channels -delete UID         delete channel with UID

  timeline UID                 show posts for channel UID
  timeline UID -after AFTER    show posts for channel UID starting from AFTER
  timeline UID -before BEFORE  show posts for channel UID ending at BEFORE

  search QUERY                 search for feeds from QUERY

  preview URL                  show items from the feed at URL

  follow UID                   show follow list for channel UID
  follow UID URL               follow URL on channel UID

  unfollow UID URL             unfollow URL on channel UID

`, os.Args[0])
		return
	}

	if len(commands) == 1 && commands[0] == "channels" {
		channels := sub.ChannelsGetList()
		for _, ch := range channels {
			fmt.Printf("%-20s %s\n", ch.UID, ch.Name)
		}
	}

	if len(commands) == 2 && commands[0] == "channels" {
		name := commands[1]
		channel := sub.ChannelsCreate(name)
		fmt.Printf("%s\n", channel.UID)
	}

	if len(commands) == 3 && commands[0] == "channels" {
		uid := commands[1]
		if uid == "-delete" {
			uid = commands[2]
			sub.ChannelsDelete(uid)
			fmt.Printf("Channel %s deleted\n", uid)
		} else {
			name := commands[2]
			channel := sub.ChannelsUpdate(uid, name)
			fmt.Printf("Channel updated %s %s\n", channel.Name, channel.UID)
		}
	}

	if len(commands) >= 2 && commands[0] == "timeline" {
		channel := commands[1]

		var timeline microsub.Timeline

		if len(commands) == 4 && commands[2] == "-after" {
			timeline = sub.TimelineGet("", commands[3], channel)
		} else if len(commands) == 4 && commands[2] == "-before" {
			timeline = sub.TimelineGet(commands[3], "", channel)
		} else {
			timeline = sub.TimelineGet("", "", channel)
		}

		for _, item := range timeline.Items {
			showItem(&item)
		}

		fmt.Printf("Before: %s, After: %s\n", timeline.Paging.Before, timeline.Paging.After)
	}

	if len(commands) == 2 && commands[0] == "search" {
		query := commands[1]
		feeds := sub.Search(query)

		for _, feed := range feeds {
			fmt.Println(feed.Name, " ", feed.URL)
		}
	}

	if len(commands) == 2 && commands[0] == "preview" {
		url := commands[1]
		timeline := sub.PreviewURL(url)

		for _, item := range timeline.Items {
			showItem(&item)
		}
	}

	if len(commands) == 2 && commands[0] == "follow" {
		uid := commands[1]
		feeds := sub.FollowGetList(uid)
		for _, feed := range feeds {
			fmt.Println(feed.Name, " ", feed.URL)
		}
	}

	if len(commands) == 3 && commands[0] == "follow" {
		uid := commands[1]
		url := commands[2]
		sub.FollowURL(uid, url)
	}

	if len(commands) == 3 && commands[0] == "unfollow" {
		uid := commands[1]
		url := commands[2]
		sub.UnfollowURL(uid, url)
	}
}

func showItem(item *microsub.Item) {
	if item.Name != "" {
		fmt.Printf("%s - ", item.Name)
	}
	fmt.Printf("%s\n", item.Published)
	if item.Content != nil {
		if item.Content.Text != "" {
			fmt.Println(item.Content.Text)
		} else {
			fmt.Println(item.Content.HTML)
		}
	}
	fmt.Println(item.URL)
	fmt.Println()
}
