/*
 *  Ekster is a microsub server
 *  Copyright (c) 2021 The Ekster authors
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 */

// Ek is a microsub client.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gilliek/go-opml/opml"
	"github.com/pstuifzand/ekster/pkg/client"
	"github.com/pstuifzand/ekster/pkg/indieauth"
	"github.com/pstuifzand/ekster/pkg/microsub"
)

// Version is overridden from the release tag when building release binaries.
var Version = "0.10.0"

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

// ExportFeed is a feed.
type ExportFeed string

// ExportChannel contains the channel information for exports.
type ExportChannel struct {
	UID  string `json:"uid,omitempty"`
	Name string `json:"channel,omitempty"`
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
	flag.Usage = func() {
		fmt.Print(`Ek is a tool for managing Microsub servers.

Usage:

	ek command [arguments]

Commands:

	connect URL                  login to a website that supports Indieauth and Microsub
	reader                       open the Ekstereye reader with Ek-managed login
	mcp                          run a stdio MCP server for the configured Microsub account

	channels                     list channels
	channels NAME                create channel with NAME
	channels UID NAME            update channel UID with NAME
	channels -delete UID         delete channel with UID

	timeline UID                 show posts for channel UID
	mark-read UID                mark all posts in channel UID as read
	timeline UID -after AFTER    show posts for channel UID starting from AFTER
	timeline UID -before BEFORE  show posts for channel UID ending at BEFORE

	search QUERY                 search for feeds from QUERY
	query QUERY CHANNEL          search for items matching QUERY in CHANNEL

	preview URL                  show items from the feed at URL

	thread TARGET                show the conversation around an entry (_id or URL)
	thread UID TARGET            the same, scoped to channel UID

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

	flag.Parse()

	configDir := fmt.Sprintf("%s/.config/microsub", os.Getenv("HOME"))

	if len(flag.Args()) == 1 && flag.Args()[0] == "reader" {
		if err := runReader(); err != nil {
			fail(err)
		}
		return
	}

	if len(os.Args) == 3 && os.Args[1] == "connect" {
		err := os.MkdirAll(configDir, os.FileMode(0770))
		if err != nil {
			fail(err)
		}

		f, err := os.Create(fmt.Sprintf("%s/client.json", configDir))
		if err != nil {
			fail(err)
		}
		defer f.Close()

		me, err := url.Parse(os.Args[2])
		if err != nil {
			fail(err)
		}

		endpoints, err := indieauth.GetEndpoints(me)
		if err != nil {
			fail(err)
		}

		clientID := "https://p83.nl/microsub-client"
		scope := "read follow mute block channels"

		token, err := indieauth.Authorize(me, endpoints, clientID, scope)
		if err != nil {
			fail(err)
		}

		enc := json.NewEncoder(f)
		err = enc.Encode(token)
		if err != nil {
			fail(err)
		}

		fmt.Println("Authorization successful")

		return
	}

	var c client.Client
	err := loadAuth(&c, fmt.Sprintf("%s/client.json", configDir))
	if err != nil {
		fail(err)
	}

	err = loadEndpoints(&c, c.Me, fmt.Sprintf("%s/endpoints.json", configDir))
	if err != nil {
		fail(err)
	}

	c.Logging = *verbose

	if len(flag.Args()) == 1 && flag.Args()[0] == "mcp" {
		// MCP stdio reserves stdout for protocol frames; do not dump HTTP
		// requests (which contain the bearer token) even under --verbose.
		c.Logging = false
		if err := runMCP(&c); err != nil {
			fail(err)
		}
		return
	}

	performCommands(&c, flag.Args())
}

func channelID(ctx context.Context, sub microsub.Microsub, channelNameOrID string) (string, error) {
	channels, err := sub.ChannelsGetList(ctx)
	if err != nil {
		// we encountered an error, so we are not sure if it worked
		return channelNameOrID, err
	}

	for _, c := range channels {
		if c.Name == channelNameOrID {
			return c.UID, nil
		}
		if c.UID == channelNameOrID {
			return c.UID, nil
		}
	}

	// unknown?
	return channelNameOrID, nil
}

// fail reports an error the way a command line tool should: one plain line on
// stderr and a non-zero exit. The CLI is read by a person at a terminal, so its
// output stays plain text rather than logfmt key=value fields.
func fail(err error) {
	fmt.Fprintf(os.Stderr, "%v\n", err)
	os.Exit(1)
}

func performCommands(sub microsub.Microsub, commands []string) {
	if len(commands) == 0 {
		flag.Usage()
		return
	}

	ctx := context.Background()

	if len(commands) == 1 && commands[0] == "channels" {
		channels, err := sub.ChannelsGetList(ctx)
		if err != nil {
			fail(err)
		}

		for _, ch := range channels {
			fmt.Printf("%-20s %-30s %s\n", ch.UID, ch.Name, ch.Unread)
		}
	}

	if len(commands) == 2 && commands[0] == "channels" {
		name := commands[1]
		channel, err := sub.ChannelsCreate(ctx, name)
		if err != nil {
			fail(err)
		}
		fmt.Printf("%s\n", channel.UID)
	}

	if len(commands) == 3 && commands[0] == "channels" {
		if commands[1] == "-delete" {
			uid, _ := channelID(ctx, sub, commands[2])
			err := sub.ChannelsDelete(ctx, uid)
			if err != nil {
				fail(err)
			}
			fmt.Printf("Channel %s deleted\n", uid)
		} else {
			uid, _ := channelID(ctx, sub, commands[1])
			name := commands[2]
			channel, err := sub.ChannelsUpdate(ctx, uid, name)
			if err != nil {
				fail(err)
			}
			fmt.Printf("Channel updated %s %s\n", channel.Name, channel.UID)
		}
	}

	if len(commands) == 2 && commands[0] == "mark-read" {
		channel, err := channelID(ctx, sub, commands[1])
		if err != nil {
			fail(err)
		}

		marked := 0
		cursor := ""
		for {
			timeline, err := sub.TimelineGet(ctx, "", cursor, channel)
			if err != nil {
				fail(err)
			}

			var ids []string
			for _, item := range timeline.Items {
				if item.ID != "" {
					ids = append(ids, item.ID)
				}
			}
			if len(ids) > 0 {
				if err := sub.MarkRead(ctx, channel, ids); err != nil {
					fail(err)
				}
				marked += len(ids)
			}

			if timeline.Paging.After == "" {
				break
			}
			if timeline.Paging.After == cursor {
				fail(fmt.Errorf("timeline paging did not advance at %s", cursor))
			}
			cursor = timeline.Paging.After
		}
		fmt.Printf("Marked %d entries as read in channel %s\n", marked, channel)
	}

	if len(commands) >= 2 && commands[0] == "timeline" {
		channel, _ := channelID(ctx, sub, commands[1])

		var timeline microsub.Timeline
		var err error

		if len(commands) == 4 && commands[2] == "-after" {
			timeline, err = sub.TimelineGet(ctx, "", commands[3], channel)
		} else if len(commands) == 4 && commands[2] == "-before" {
			timeline, err = sub.TimelineGet(ctx, commands[3], "", channel)
		} else {
			timeline, err = sub.TimelineGet(ctx, "", "", channel)
		}

		if err != nil {
			fail(err)
		}

		for _, item := range timeline.Items {
			showItem(&item)
		}

		fmt.Printf("Before: %s, After: %s\n", timeline.Paging.Before, timeline.Paging.After)
	}

	if len(commands) >= 2 && commands[0] == "thread" {
		channel := ""
		target := commands[1]
		if len(commands) == 3 {
			var err error
			channel, err = channelID(ctx, sub, commands[1])
			if err != nil {
				fail(err)
			}
			target = commands[2]
		} else if len(commands) > 3 {
			fail(fmt.Errorf("thread takes a target, or a channel and a target"))
		}

		var thread microsub.Thread
		var err error
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			thread, err = sub.ThreadGet(ctx, channel, "", target, 0)
		} else {
			thread, err = sub.ThreadGet(ctx, channel, target, "", 0)
		}
		if err != nil {
			fail(err)
		}

		for i := range thread.Items {
			showItem(&thread.Items[i])
		}

		status := "complete"
		if thread.Thread.Partial {
			status = fmt.Sprintf("partial, %d ancestor(s) not shown", thread.Thread.Missing)
		}
		fmt.Printf("Thread: %s (%s)\n", thread.Thread.Root, status)
	}

	if len(commands) == 2 && commands[0] == "search" {
		query := commands[1]
		feeds, err := sub.Search(ctx, query)
		if err != nil {
			fail(err)
		}

		for _, feed := range feeds {
			fmt.Println(feed.Name, " ", feed.URL)
		}
	}

	if len(commands) >= 2 && len(commands) <= 3 && commands[0] == "query" {
		query := commands[1]
		var channel string
		if len(commands) == 3 {
			channel, _ = channelID(ctx, sub, commands[2])
		} else {
			channel = "global"
		}
		items, err := sub.ItemSearch(ctx, channel, query)
		if err != nil {
			fail(err)
		}

		for _, item := range items {
			showItem(&item)
		}
	}

	if len(commands) == 2 && commands[0] == "preview" {
		u := commands[1]
		timeline, err := sub.PreviewURL(ctx, u)

		if err != nil {
			fail(err)
		}
		for _, item := range timeline.Items {
			showItem(&item)
		}
	}

	if len(commands) == 2 && commands[0] == "follow" {
		uid, _ := channelID(ctx, sub, commands[1])
		feeds, err := sub.FollowGetList(ctx, uid)
		if err != nil {
			fail(err)
		}
		for _, feed := range feeds {
			fmt.Println(feed.URL)
		}
	}

	if len(commands) == 3 && commands[0] == "follow" {
		uid, _ := channelID(ctx, sub, commands[1])
		u := commands[2]
		_, err := sub.FollowURL(ctx, uid, u)
		if err != nil {
			fail(err)
		}
		// NOTE(peter): should we show the returned feed here?
	}

	if len(commands) == 3 && commands[0] == "unfollow" {
		uid, _ := channelID(ctx, sub, commands[1])
		u := commands[2]
		err := sub.UnfollowURL(ctx, uid, u)
		if err != nil {
			fail(err)
		}
	}

	if len(commands) == 2 && commands[0] == "export" {
		filetype := commands[1]

		if filetype == "opml" {
			exportOPMLFromMicrosub(ctx, sub)
		} else if filetype == "json" {
			exportJSONFromMicrosub(ctx, sub)
		} else {
			fail(fmt.Errorf("unsupported filetype %q", filetype))
		}
	}

	if len(commands) == 3 && commands[0] == "import" {
		filetype := commands[1]
		filename := commands[2]

		if filetype == "opml" {
			importOPMLIntoMicrosub(ctx, sub, filename)
		} else if filetype == "json" {
			importJSONIntoMicrosub(ctx, sub, filename)
		} else {
			fail(fmt.Errorf("unsupported filetype %q", filetype))
		}
	}

	if len(commands) == 1 && commands[0] == "version" {
		fmt.Printf("ek %s\n", Version)
	}

	if len(commands) == 1 && commands[0] == "events" {
		c, err := sub.Events(ctx)
		if err != nil {
			fail(fmt.Errorf("could not start event listener: %w", err))
		}
		for msg := range c {
			fmt.Printf("%s: %s\n", msg.Event, msg.Data)
		}
	}
}

func exportOPMLFromMicrosub(ctx context.Context, sub microsub.Microsub) {
	output := opml.OPML{}
	output.Head.Title = "Microsub channels and feeds"
	output.Head.DateCreated = time.Now().Format(time.RFC3339)
	output.Version = "1.0"
	channels, err := sub.ChannelsGetList(ctx)
	if err != nil {
		fail(err)
	}
	for _, c := range channels {
		var feeds []opml.Outline
		list, err := sub.FollowGetList(ctx, c.UID)
		if err != nil {
			fail(err)
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
		fail(err)
	}
	os.Stdout.WriteString(xml)
}

func exportJSONFromMicrosub(ctx context.Context, sub microsub.Microsub) {
	contents := Export{Version: "1.0", Generator: "ek version " + Version}
	channels, err := sub.ChannelsGetList(ctx)
	if err != nil {
		fail(err)
	}
	for _, c := range channels {
		contents.Channels = append(contents.Channels, ExportChannel{UID: c.UID, Name: c.Name})
	}
	contents.Feeds = make(map[string][]ExportFeed)
	for _, c := range channels {
		list, err := sub.FollowGetList(ctx, c.UID)
		if err != nil {
			fail(err)
		}
		for _, f := range list {
			contents.Feeds[c.UID] = append(contents.Feeds[c.UID], ExportFeed(f.URL))
		}
	}
	err = json.NewEncoder(os.Stdout).Encode(&contents)
	if err != nil {
		fail(err)
	}
}

func importJSONIntoMicrosub(ctx context.Context, sub microsub.Microsub, filename string) {
	var export Export
	f, err := os.Open(filename)
	if err != nil {
		fail(fmt.Errorf("could not open %s: %w", filename, err))
	}
	defer f.Close()
	err = json.NewDecoder(f).Decode(&export)
	if err != nil {
		fail(fmt.Errorf("could not read %s: %w", filename, err))
	}
	channelMap := make(map[string]microsub.Channel)
	channels, err := sub.ChannelsGetList(ctx)
	if err != nil {
		fail(err)
	}
	for _, c := range channels {
		channelMap[c.Name] = c
	}
	for _, c := range export.Channels {
		uid := ""

		if ch, e := channelMap[c.Name]; !e {
			channelCreated, err := sub.ChannelsCreate(ctx, c.Name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not create channel %s: %v\n", c.Name, err)
				continue
			}

			uid = channelCreated.UID
			fmt.Printf("Channel created: %s\n", c.Name)
		} else {
			uid = ch.UID
		}

		feedMap := make(map[string]bool)

		feeds, err := sub.FollowGetList(ctx, uid)
		if err != nil {
			fail(err)
		}

		for _, f := range feeds {
			feedMap[f.URL] = true
		}

		for _, feed := range export.Feeds[uid] {

			if _, e := feedMap[string(feed)]; !e {
				_, err := sub.FollowURL(ctx, uid, string(feed))
				if err != nil {
					fmt.Fprintf(os.Stderr, "could not follow %s: %v\n", feed, err)
					continue
				}
				fmt.Printf("Feed followed: %s\n", string(feed))
			}
		}
	}
}

func importOPMLIntoMicrosub(ctx context.Context, sub microsub.Microsub, filename string) {
	channelMap := make(map[string]microsub.Channel)
	channels, err := sub.ChannelsGetList(ctx)
	if err != nil {
		fail(err)
	}
	for _, c := range channels {
		channelMap[c.Name] = c
	}
	xml, err := opml.NewOPMLFromFile(filename)
	if err != nil {
		fail(err)
	}
	for _, c := range xml.Body.Outlines {
		if c.HTMLURL != "" {
			fmt.Printf("First row item has url: %s\n", c.HTMLURL)
			continue
		}
		if len(c.Outlines) == 0 {
			continue
		}

		uid := ""

		if ch, e := channelMap[c.Text]; !e {
			channelCreated, err := sub.ChannelsCreate(ctx, c.Text)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not create channel %s: %v\n", c.Text, err)
				continue
			}

			uid = channelCreated.UID
			fmt.Printf("Channel created: %s\n", c.Text)
		} else {
			uid = ch.UID
		}

		feedMap := make(map[string]bool)

		feeds, err := sub.FollowGetList(ctx, uid)
		if err != nil {
			fail(err)
		}

		for _, f := range feeds {
			feedMap[f.URL] = true
		}

		for _, f := range c.Outlines {
			var url string

			if f.HTMLURL != "" {
				url = f.HTMLURL
			} else if f.XMLURL != "" {
				url = f.XMLURL
			} else {
				fmt.Fprintln(os.Stderr, "missing htmlUrl and xmlUrl attributes")
				continue
			}

			if _, e := feedMap[url]; !e {
				_, err := sub.FollowURL(ctx, uid, url)
				if err != nil {
					fmt.Fprintf(os.Stderr, "could not follow %s: %v\n", url, err)
					continue
				}

				fmt.Printf("Feed followed: %s\n", url)
			} else {
				fmt.Printf("Feed not followed: %s\n", url)
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
