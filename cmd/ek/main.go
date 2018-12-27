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

const (
	// Version is the version of the command
	Version = "0.8.3"
)

var (
	verbose = flag.Bool("verbose", false, "show verbose logging")
)

// Export is the JSON export format
type Export struct {
	Version   string                  `json:"version"`
	Generator string                  `json:"generator"`
	Channels  []ExportChannel         `json:"channels,omitempty"`
	Feeds     map[string][]ExportFeed `json:"feeds,omitempty"`
}

type ExportFeed string

type ExportChannel struct {
	UID  string `json:"uid,omitempty"`
	Name string `json:"channel,omitempty"`
}

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
		return fmt.Errorf("microsub endpoint is missing")
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

	connect URL                  login to Indieauth URL, e.g. your website

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
	import opml FILENAME         import OPML feeds

	export json                  export feeds as json
	import json FILENAME         import json feeds

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
		u := commands[1]
		timeline, err := sub.PreviewURL(u)

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
		u := commands[2]
		_, err := sub.FollowURL(uid, u)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
		// NOTE(peter): should we show the returned feed here?
	}

	if len(commands) == 3 && commands[0] == "unfollow" {
		uid := commands[1]
		u := commands[2]
		err := sub.UnfollowURL(uid, u)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
	}

	if len(commands) == 2 && commands[0] == "export" {
		filetype := commands[1]

		if filetype == "opml" {
			exportOpmlFromMicrosub(sub)
		} else if filetype == "json" {
			exportJsonFromMicrosub(sub)
		} else {
			log.Fatalf("unsupported filetype %q", filetype)
		}
	}

	if len(commands) == 3 && commands[0] == "import" {
		filetype := commands[1]
		filename := commands[2]

		if filetype == "opml" {
			importOpmlIntoMicrosub(sub, filename)
		} else if filetype == "json" {
			importJsonIntoMicrosub(sub, filename)
		} else {
			log.Fatalf("unsupported filetype %q", filetype)
		}
	}

	if len(commands) == 1 && commands[0] == "version" {
		fmt.Printf("ek %s\n", Version)
	}
}

func exportOpmlFromMicrosub(sub microsub.Microsub) {
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
}

func exportJsonFromMicrosub(sub microsub.Microsub) {
	contents := Export{Version: "1.0", Generator: "ek version " + Version}
	channels, err := sub.ChannelsGetList()
	if err != nil {
		log.Fatalf("An error occurred: %s\n", err)
	}
	for _, c := range channels {
		contents.Channels = append(contents.Channels, ExportChannel{UID: c.UID, Name: c.Name})
	}
	contents.Feeds = make(map[string][]ExportFeed)
	for _, c := range channels {
		list, err := sub.FollowGetList(c.UID)
		if err != nil {
			log.Fatalf("An error occurred: %s\n", err)
		}
		for _, f := range list {
			contents.Feeds[c.UID] = append(contents.Feeds[c.UID], ExportFeed(f.URL))
		}
	}
	err = json.NewEncoder(os.Stdout).Encode(&contents)
	if err != nil {
		log.Fatalf("An error occurred: %s\n", err)
	}
}

func importJsonIntoMicrosub(sub microsub.Microsub, filename string) {
	var export Export
	f, err := os.Open(filename)
	if err != nil {
		log.Fatalf("can't open file %s: %s", filename, err)
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&export)
	if err != nil {
		log.Fatalf("error while reading %s: %s", filename, err)
	}
	channelMap := make(map[string]microsub.Channel)
	channels, err := sub.ChannelsGetList()
	if err != nil {
		log.Fatalf("an error occurred: %s\n", err)
	}
	for _, c := range channels {
		channelMap[c.Name] = c
	}
	for _, c := range export.Channels {
		uid := ""

		if ch, e := channelMap[c.Name]; !e {
			channelCreated, err := sub.ChannelsCreate(c.Name)
			if err != nil {
				log.Printf("An error occurred: %q\n", err)
				continue
			}

			uid = channelCreated.UID
			log.Printf("Channel created: %s\n", c.Name)
		} else {
			uid = ch.UID
		}

		feedMap := make(map[string]bool)

		feeds, err := sub.FollowGetList(uid)
		if err != nil {
			log.Fatalf("An error occurred: %q\n", err)
		}

		for _, f := range feeds {
			feedMap[f.URL] = true
		}

		for _, feed := range export.Feeds[uid] {

			if _, e := feedMap[string(feed)]; !e {
				_, err := sub.FollowURL(uid, string(feed))
				if err != nil {
					log.Printf("An error occurred: %q\n", err)
					continue
				}
				log.Printf("Feed followed: %s\n", string(feed))
			}
		}
	}
}

func importOpmlIntoMicrosub(sub microsub.Microsub, filename string) {
	channelMap := make(map[string]microsub.Channel)
	channels, err := sub.ChannelsGetList()
	if err != nil {
		log.Fatalf("an error occurred: %s\n", err)
	}
	for _, c := range channels {
		channelMap[c.Name] = c
	}
	xml, err := opml.NewOPMLFromFile(filename)
	if err != nil {
		log.Fatalf("An error occurred: %s\n", err)
	}
	for _, c := range xml.Body.Outlines {
		if c.HTMLURL != "" {
			log.Printf("First row item has url: %s\n", c.HTMLURL)
			continue
		}
		if len(c.Outlines) == 0 {
			continue
		}

		uid := ""

		if ch, e := channelMap[c.Text]; !e {
			channelCreated, err := sub.ChannelsCreate(c.Text)
			if err != nil {
				log.Printf("An error occurred: %q\n", err)
				continue
			}

			uid = channelCreated.UID
			log.Printf("Channel created: %s\n", c.Text)
		} else {
			uid = ch.UID
		}

		feedMap := make(map[string]bool)

		feeds, err := sub.FollowGetList(uid)
		if err != nil {
			log.Fatalf("An error occurred: %q\n", err)
		}

		for _, f := range feeds {
			feedMap[f.URL] = true
		}

		for _, f := range c.Outlines {
			if f.HTMLURL == "" {
				log.Println("Missing url on second row item")
				continue
			}

			if _, e := feedMap[f.HTMLURL]; !e {
				_, err := sub.FollowURL(uid, f.HTMLURL)
				if err != nil {
					log.Printf("An error occurred: %q\n", err)
					continue
				}
				log.Printf("Feed followed: %s\n", f.HTMLURL)
			}
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
