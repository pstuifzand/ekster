/*
   ek - microsub client
   Copyright (C) 2018  Peter Stuifzand

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/gilliek/go-opml/opml"
	"p83.nl/go/ekster/pkg/client"
	"p83.nl/go/ekster/pkg/indieauth"
	"p83.nl/go/ekster/pkg/microsub"
)

var (
	verbose = flag.Bool("verbose", false, "show verbose logging")
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
	flag.Parse()

	flag.Usage = func() {
		fmt.Print(`Ek is a tool for managing Microsub servers.

Usage:

	ek command [arguments]

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

	export opml                  export feeds as OPML

Global arguments:

`)
		flag.PrintDefaults()
	}

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

	c.Logging = *verbose

	performCommands(&c, flag.Args())
}

func performCommands(sub microsub.Microsub, commands []string) {
	if len(commands) == 0 {
		flag.Usage()
		return
	}

	if len(commands) == 1 && commands[0] == "channels" {
		channels, err := sub.ChannelsGetList()
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}

		for _, ch := range channels {
			fmt.Printf("%-20s %s\n", ch.UID, ch.Name)
		}
	}

	if len(commands) == 2 && commands[0] == "channels" {
		name := commands[1]
		channel, err := sub.ChannelsCreate(name)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
		fmt.Printf("%s\n", channel.UID)
	}

	if len(commands) == 3 && commands[0] == "channels" {
		uid := commands[1]
		if uid == "-delete" {
			uid = commands[2]
			err := sub.ChannelsDelete(uid)
			if err != nil {
				log.Fatalf("An error occurred: %s\n", err)
			}
			fmt.Printf("Channel %s deleted\n", uid)
		} else {
			name := commands[2]
			channel, err := sub.ChannelsUpdate(uid, name)
			if err != nil {
				log.Fatalf("An error occurred: %s\n", err)
			}
			fmt.Printf("Channel updated %s %s\n", channel.Name, channel.UID)
		}
	}

	if len(commands) >= 2 && commands[0] == "timeline" {
		channel := commands[1]

		var timeline microsub.Timeline
		var err error

		if len(commands) == 4 && commands[2] == "-after" {
			timeline, err = sub.TimelineGet("", commands[3], channel)
		} else if len(commands) == 4 && commands[2] == "-before" {
			timeline, err = sub.TimelineGet(commands[3], "", channel)
		} else {
			timeline, err = sub.TimelineGet("", "", channel)
		}

		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}

		for _, item := range timeline.Items {
			showItem(&item)
		}

		fmt.Printf("Before: %s, After: %s\n", timeline.Paging.Before, timeline.Paging.After)
	}

	if len(commands) == 2 && commands[0] == "search" {
		query := commands[1]
		feeds, err := sub.Search(query)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}

		for _, feed := range feeds {
			fmt.Println(feed.Name, " ", feed.URL)
		}
	}

	if len(commands) == 2 && commands[0] == "preview" {
		url := commands[1]
		timeline, err := sub.PreviewURL(url)

		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
		for _, item := range timeline.Items {
			showItem(&item)
		}
	}

	if len(commands) == 2 && commands[0] == "follow" {
		uid := commands[1]
		feeds, err := sub.FollowGetList(uid)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
		for _, feed := range feeds {
			fmt.Println(feed.URL)
		}
	}

	if len(commands) == 3 && commands[0] == "follow" {
		uid := commands[1]
		url := commands[2]
		_, err := sub.FollowURL(uid, url)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
		// NOTE(peter): should we show the returned feed here?
	}

	if len(commands) == 3 && commands[0] == "unfollow" {
		uid := commands[1]
		url := commands[2]
		err := sub.UnfollowURL(uid, url)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
	}

	if len(commands) == 2 && commands[0] == "export" {
		filetype := commands[1]
		if filetype == "opml" {
			output := opml.OPML{}
			output.Head.Title = "Microsub channels and feeds"
			output.Head.DateCreated = time.Now().Format(time.RFC3339)
			output.Version = "1.0"

			channels, err := sub.ChannelsGetList()
			if err != nil {
				log.Fatalf("An error occurred: %s\n", err)
			}

			for _, c := range channels {
				var feeds []opml.Outline
				list, err := sub.FollowGetList(c.UID)
				if err != nil {
					log.Fatalf("An error occurred: %s\n", err)
				}
				for _, f := range list {
					feeds = append(feeds, opml.Outline{
						Title:   f.Name,
						Text:    f.Name,
						Type:    f.Type,
						URL:     f.URL,
						HTMLURL: f.URL,
						XMLURL:  f.URL,
					})
				}

				output.Body.Outlines = append(output.Body.Outlines, opml.Outline{
					Text:     c.Name,
					Title:    c.Name,
					Outlines: feeds,
				})
			}

			xml, err := output.XML()
			if err != nil {
				log.Fatalf("An error occurred: %s\n", err)
			}
			os.Stdout.WriteString(xml)
		} else {
			log.Fatalf("unsupported filetype %q", filetype)
		}
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
